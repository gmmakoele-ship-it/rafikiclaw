//go:build integration

package manager_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gmmakoele-ship-it/rafikiclaw/internal/manager"
)

const (
	integrationImage = "alpine:3.20@sha256:a4f4213abb84c497377b8544c81b3564f313746700372ec4fe84653e4fb03805"
)

func TestE2ERuntimeEphemeralSuccess(t *testing.T) {
	runtimeTarget := requireHealthyRuntime(t)
	ensureImageAvailable(t, runtimeTarget, integrationImage)

	stateDir := t.TempDir()
	clawPath := writeClawfile(t, stateDir, clawSpec{
		Name:      "e2e-ephemeral",
		Lifecycle: "ephemeral",
		Runtime:   runtimeTarget,
		Image:     integrationImage,
		Command:   "echo E2E_EPHEMERAL_OK",
	})

	m, err := manager.New(stateDir)
	if err != nil {
		t.Fatalf("manager.New() error = %v", err)
	}
	defer m.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	rec, err := m.Run(ctx, manager.RunOptions{InputPath: clawPath})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if rec.Status != "succeeded" {
		t.Fatalf("expected succeeded status, got %q", rec.Status)
	}
	if rec.RuntimeTarget != runtimeTarget {
		t.Fatalf("expected runtime %q, got %q", runtimeTarget, rec.RuntimeTarget)
	}

	saved, err := m.GetRun(rec.RunID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if saved.ExitCode == nil || *saved.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %+v", saved.ExitCode)
	}

	stdoutPath := filepath.Join(stateDir, "runs", rec.RunID, "stdout.log")
	stdout, err := os.ReadFile(stdoutPath)
	if err != nil {
		t.Fatalf("read stdout log: %v", err)
	}
	if !strings.Contains(string(stdout), "E2E_EPHEMERAL_OK") {
		t.Fatalf("expected stdout token, got: %s", string(stdout))
	}

	events, err := m.ReadEvents(rec.RunID)
	if err != nil {
		t.Fatalf("ReadEvents() error = %v", err)
	}
	joined := strings.Join(events, "\n")
	if !strings.Contains(joined, `"phase":"runtime.exit"`) {
		t.Fatalf("expected runtime.exit event, got: %s", joined)
	}

	if err := expectContainerGone(runtimeTarget, rec.ContainerID); err != nil {
		t.Fatalf("expected container to be removed: %v", err)
	}
}

func TestE2ERuntimeDaemonLifecycle(t *testing.T) {
	runtimeTarget := requireHealthyRuntime(t)
	ensureImageAvailable(t, runtimeTarget, integrationImage)

	stateDir := t.TempDir()
	clawPath := writeClawfile(t, stateDir, clawSpec{
		Name:      "e2e-daemon",
		Lifecycle: "daemon",
		Runtime:   runtimeTarget,
		Image:     integrationImage,
		Command:   "sleep 30",
	})

	m, err := manager.New(stateDir)
	if err != nil {
		t.Fatalf("manager.New() error = %v", err)
	}
	defer m.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	rec, err := m.Run(ctx, manager.RunOptions{InputPath: clawPath})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if rec.Status != "running" {
		t.Fatalf("expected running status, got %q", rec.Status)
	}
	if rec.ContainerID == "" {
		t.Fatal("expected non-empty container ID")
	}
	defer cleanupContainer(t, runtimeTarget, rec.ContainerID)

	saved, err := m.GetRun(rec.RunID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if saved.Status != "running" {
		t.Fatalf("expected stored status running, got %q", saved.Status)
	}

	if _, err := inspectContainer(runtimeTarget, rec.ContainerID); err != nil {
		t.Fatalf("expected running container to be inspectable: %v", err)
	}
}

func TestE2EDaemonStatusReconcilesAfterContainerExit(t *testing.T) {
	runtimeTarget := requireHealthyRuntime(t)
	ensureImageAvailable(t, runtimeTarget, integrationImage)

	stateDir := t.TempDir()
	clawPath := writeClawfile(t, stateDir, clawSpec{
		Name:      "e2e-daemon-exit",
		Lifecycle: "daemon",
		Runtime:   runtimeTarget,
		Image:     integrationImage,
		Command:   "sleep 1",
	})

	m, err := manager.New(stateDir)
	if err != nil {
		t.Fatalf("manager.New() error = %v", err)
	}
	defer m.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	rec, err := m.Run(ctx, manager.RunOptions{InputPath: clawPath})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if rec.ContainerID == "" {
		t.Fatal("expected non-empty container ID")
	}
	defer cleanupContainer(t, runtimeTarget, rec.ContainerID)

	time.Sleep(2 * time.Second)

	saved, err := m.GetRun(rec.RunID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if saved.Status != "succeeded" {
		t.Fatalf("expected status to reconcile to succeeded after daemon exit, got %q", saved.Status)
	}
	if saved.ExitCode == nil || *saved.ExitCode != 0 {
		t.Fatalf("expected reconciled exit code 0, got %+v", saved.ExitCode)
	}
}

func TestE2ERuntimeDebugPauseOnFailure(t *testing.T) {
	runtimeTarget := requireHealthyRuntime(t)
	ensureImageAvailable(t, runtimeTarget, integrationImage)

	stateDir := t.TempDir()
	clawPath := writeClawfile(t, stateDir, clawSpec{
		Name:      "e2e-debug-fail",
		Lifecycle: "debug",
		Runtime:   runtimeTarget,
		Image:     integrationImage,
		Command:   "echo E2E_DEBUG_FAIL && exit 17",
	})

	m, err := manager.New(stateDir)
	if err != nil {
		t.Fatalf("manager.New() error = %v", err)
	}
	defer m.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	rec, err := m.Run(ctx, manager.RunOptions{InputPath: clawPath})
	if err == nil {
		t.Fatal("expected Run() to return an error for debug failure case")
	}
	if rec.Status != "failed_paused" {
		t.Fatalf("expected failed_paused status, got %q", rec.Status)
	}
	if rec.ContainerID == "" {
		t.Fatal("expected container to be preserved with non-empty ID")
	}
	defer cleanupContainer(t, runtimeTarget, rec.ContainerID)

	saved, err := m.GetRun(rec.RunID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if saved.Status != "failed_paused" {
		t.Fatalf("expected stored status failed_paused, got %q", saved.Status)
	}

	if _, err := inspectContainer(runtimeTarget, rec.ContainerID); err != nil {
		t.Fatalf("expected preserved debug container to be inspectable: %v", err)
	}

	events, err := m.ReadEvents(rec.RunID)
	if err != nil {
		t.Fatalf("ReadEvents() error = %v", err)
	}
	joined := strings.Join(events, "\n")
	if !strings.Contains(joined, `"phase":"runtime.pause"`) {
		t.Fatalf("expected runtime.pause event, got: %s", joined)
	}
}

func TestE2ERuntimeOverridePrecedence(t *testing.T) {
	available := healthyRuntimes()
	if len(available) == 0 {
		t.Skip("no healthy runtime (docker/podman) is available for E2E")
	}
	override := available[0]
	fileRuntime := pickDifferentRuntime(override, available)
	if fileRuntime == "" {
		t.Skip("could not determine a distinct runtime target for precedence test")
	}
	ensureImageAvailable(t, override, integrationImage)

	stateDir := t.TempDir()
	clawPath := writeClawfile(t, stateDir, clawSpec{
		Name:      "e2e-precedence",
		Lifecycle: "ephemeral",
		Runtime:   fileRuntime,
		Image:     integrationImage,
		Command:   "echo E2E_PRECEDENCE_OK",
	})

	m, err := manager.New(stateDir)
	if err != nil {
		t.Fatalf("manager.New() error = %v", err)
	}
	defer m.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	rec, err := m.Run(ctx, manager.RunOptions{InputPath: clawPath, RuntimeOverride: override})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if rec.RuntimeTarget != override {
		t.Fatalf("expected runtime override %q to win over clawfile target %q; got %q", override, fileRuntime, rec.RuntimeTarget)
	}
}

type clawSpec struct {
	Name      string
	Lifecycle string
	Runtime   string
	Image     string
	Command   string
}

func writeClawfile(t *testing.T, dir string, spec clawSpec) string {
	t.Helper()
	path := filepath.Join(dir, spec.Name+".claw")
	content := fmt.Sprintf(`apiVersion: metaclaw/v1
kind: Agent
agent:
  name: %s
  species: nano
  lifecycle: %s
  habitat:
    network:
      mode: none
  runtime:
    target: %s
    image: %s
  command:
    - sh
    - -lc
    - %q
`, spec.Name, spec.Lifecycle, spec.Runtime, spec.Image, spec.Command)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write clawfile: %v", err)
	}
	return path
}

func requireHealthyRuntime(t *testing.T) string {
	t.Helper()
	if r := os.Getenv("METACLAW_TEST_RUNTIME"); r != "" {
		if isRuntimeHealthy(r) {
			return r
		}
		t.Skipf("METACLAW_TEST_RUNTIME=%s is not healthy/available", r)
	}
	available := healthyRuntimes()
	if len(available) == 0 {
		t.Skip("no healthy runtime (docker/podman) available")
	}
	return available[0]
}

func healthyRuntimes() []string {
	var out []string
	for _, r := range []string{"podman", "docker"} {
		if isRuntimeHealthy(r) {
			out = append(out, r)
		}
	}
	return out
}

func isRuntimeHealthy(runtimeTarget string) bool {
	if _, err := exec.LookPath(runtimeTarget); err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, runtimeTarget, "info")
	if err := cmd.Run(); err != nil {
		return false
	}
	return true
}

func ensureImageAvailable(t *testing.T, runtimeTarget string, image string) {
	t.Helper()
	if _, err := runRuntimeCmd(runtimeTarget, 20*time.Second, "image", "inspect", image); err == nil {
		return
	}
	if _, err := runRuntimeCmd(runtimeTarget, 2*time.Minute, "pull", image); err != nil {
		t.Skipf("unable to make image available on %s: %v", runtimeTarget, err)
	}
}

func inspectContainer(runtimeTarget string, containerID string) (string, error) {
	return runRuntimeCmd(runtimeTarget, 20*time.Second, "inspect", containerID)
}

func expectContainerGone(runtimeTarget string, containerID string) error {
	_, err := inspectContainer(runtimeTarget, containerID)
	if err == nil {
		return fmt.Errorf("container %s still exists", containerID)
	}
	return nil
}

func cleanupContainer(t *testing.T, runtimeTarget string, containerID string) {
	t.Helper()
	if containerID == "" {
		return
	}
	_, _ = runRuntimeCmd(runtimeTarget, 30*time.Second, "rm", "-f", containerID)
}

func runRuntimeCmd(runtimeTarget string, timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, runtimeTarget, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s %s failed: %w; output=%s", runtimeTarget, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func pickDifferentRuntime(override string, available []string) string {
	for _, r := range available {
		if r != override {
			return r
		}
	}
	for _, r := range []string{"podman", "docker"} {
		if r != override {
			return r
		}
	}
	return ""
}
