package manager

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/gmmakoele-ship-it/rafikiclaw/internal/capsule"
	v1 "github.com/gmmakoele-ship-it/rafikiclaw/internal/claw/schema/v1"
	"github.com/gmmakoele-ship-it/rafikiclaw/internal/compiler"
	"github.com/gmmakoele-ship-it/rafikiclaw/internal/llm"
	"github.com/gmmakoele-ship-it/rafikiclaw/internal/logs"
	"github.com/gmmakoele-ship-it/rafikiclaw/internal/policy"
	"github.com/gmmakoele-ship-it/rafikiclaw/internal/runtime"
	"github.com/gmmakoele-ship-it/rafikiclaw/internal/runtime/spec"
	store "github.com/gmmakoele-ship-it/rafikiclaw/internal/store/sqlite"
)

type Manager struct {
	stateDir string
	store    *store.Store
	resolver *runtime.Resolver
}

type RunOptions struct {
	InputPath       string
	Detach          bool
	RuntimeOverride string
	LLMAPIKey       string
	LLMAPIKeyEnv    string
	SecretEnvs      []string
}

type RunOutcome struct {
	Run   store.RunRecord
	Error error
}

func New(stateDir string) (*Manager, error) {
	if stateDir == "" {
		stateDir = ".rafikiclaw"
	}
	s, err := store.Open(stateDir)
	if err != nil {
		return nil, err
	}
	return &Manager{stateDir: stateDir, store: s, resolver: runtime.NewResolver()}, nil
}

func (m *Manager) Close() error {
	if m == nil {
		return nil
	}
	return m.store.Close()
}

func (m *Manager) Run(ctx context.Context, opts RunOptions) (store.RunRecord, error) {
	cfg, pol, capPath, capID, err := m.prepareCapsule(opts.InputPath)
	if err != nil {
		return store.RunRecord{}, err
	}
	if err := m.store.UpsertCapsule(capID, capPath); err != nil {
		return store.RunRecord{}, err
	}

	adapter, target, err := m.resolver.Resolve(ctx, opts.RuntimeOverride, string(cfg.Agent.Runtime.Target))
	if err != nil {
		return store.RunRecord{}, err
	}
	resolvedLLM, err := llm.Resolve(cfg.Agent.LLM, llm.RuntimeOptions{
		APIKey:    opts.LLMAPIKey,
		APIKeyEnv: opts.LLMAPIKeyEnv,
	})
	if err != nil {
		return store.RunRecord{}, err
	}
	resolvedSecrets, err := resolveHostSecretEnvs(opts.SecretEnvs)
	if err != nil {
		return store.RunRecord{}, err
	}
	env := mergeEnv(cfg.Agent.Habitat.Env, resolvedLLM.Env, resolvedSecrets)
	allowed := make(map[string]struct{}, len(pol.EnvAllowlist))
	for _, k := range pol.EnvAllowlist {
		allowed[k] = struct{}{}
	}
	for k := range resolvedSecrets {
		if _, ok := allowed[k]; !ok {
			return store.RunRecord{}, fmt.Errorf("secret env %s is not allowlisted by agent policy (declare it in agent.habitat.env to inject at runtime)", k)
		}
	}
	for k := range resolvedLLM.Env {
		if _, ok := allowed[k]; !ok {
			// This should not happen: llm.AllowedEnvKeys is part of the allowlist computation.
			return store.RunRecord{}, fmt.Errorf("internal error: llm env %s is not allowlisted by agent policy", k)
		}
	}
	env = filterEnvAllowlist(env, allowed)

	runID := makeRunID()
	rec := store.RunRecord{
		RunID:         runID,
		CapsuleID:     capID,
		CapsulePath:   capPath,
		AgentName:     cfg.Agent.Name,
		Status:        "running",
		Lifecycle:     string(cfg.Agent.Lifecycle),
		RuntimeTarget: string(target),
		StartedAt:     time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := m.store.InsertRun(rec); err != nil {
		return store.RunRecord{}, err
	}
	_ = logs.AppendEvent(m.stateDir, runID, logs.Event{Phase: "runtime.resolve", Runtime: string(target), Message: "runtime selected"})

	containerName := "metaclaw_" + runID
	runRes, runErr := adapter.Run(ctx, spec.RunOptions{
		ContainerName: containerName,
		Image:         cfg.Agent.Runtime.Image,
		Command:       cfg.Agent.Command,
		Detach:        opts.Detach || cfg.Agent.Lifecycle == v1.LifecycleDaemon,
		Policy:        pol,
		Env:           env,
		Workdir:       cfg.Agent.Habitat.Workdir,
		User:          cfg.Agent.Habitat.User,
		CPU:           cfg.Agent.Runtime.Resources.CPU,
		Memory:        cfg.Agent.Runtime.Resources.Memory,
	})

	containerID := runRes.ContainerID
	if containerID == "" {
		containerID = containerName
	}
	rec.ContainerID = containerID
	_ = writeRunOutput(m.stateDir, runID, "stdout.log", runRes.Stdout)
	_ = writeRunOutput(m.stateDir, runID, "stderr.log", runRes.Stderr)

	detached := opts.Detach || cfg.Agent.Lifecycle == v1.LifecycleDaemon
	if detached {
		if runErr != nil {
			errText := runErr.Error()
			_ = logs.AppendEvent(m.stateDir, runID, logs.Event{Phase: "runtime.start", Runtime: string(target), ContainerID: containerID, Message: "daemon start failed", Error: errText})
			_ = m.store.UpdateRunCompletion(runID, "failed", containerID, intPtr(runRes.ExitCode), errText)
			rec.Status = "failed"
			rec.LastError = errText
			rec.ExitCode = intPtr(runRes.ExitCode)
			return rec, runErr
		}
		_ = logs.AppendEvent(m.stateDir, runID, logs.Event{Phase: "runtime.start", Runtime: string(target), ContainerID: containerID, Message: "daemon started"})
		_ = m.store.UpdateRunStatus(runID, "running", containerID, "")
		rec.Status = "running"
		rec.ContainerID = containerID
		refreshed, refreshErr := m.refreshRunStatus(ctx, rec)
		if refreshErr == nil {
			rec = refreshed
		}
		if rec.Status == "failed" {
			if rec.LastError != "" {
				return rec, fmt.Errorf("%s", rec.LastError)
			}
			if rec.ExitCode != nil {
				return rec, fmt.Errorf("detached run failed with exit code %d", *rec.ExitCode)
			}
			return rec, fmt.Errorf("detached run failed")
		}
		return rec, nil
	}

	status := "succeeded"
	var lastError string
	exitPtr := intPtr(runRes.ExitCode)
	if runErr != nil || runRes.ExitCode != 0 {
		status = "failed"
		if runErr != nil {
			lastError = runErr.Error()
		}
	}

	if status == "failed" && cfg.Agent.Lifecycle == v1.LifecycleDebug {
		status = "failed_paused"
		_ = logs.AppendEvent(m.stateDir, runID, logs.Event{Phase: "runtime.pause", Runtime: string(target), ContainerID: containerID, Message: "container preserved for debug", Error: lastError})
	} else {
		if remErr := adapter.Remove(ctx, containerID); remErr == nil {
			_ = logs.AppendEvent(m.stateDir, runID, logs.Event{Phase: "runtime.cleanup", Runtime: string(target), ContainerID: containerID, Message: "container removed"})
		}
	}

	_ = m.store.UpdateRunCompletion(runID, status, containerID, exitPtr, lastError)
	rec.Status = status
	rec.ExitCode = exitPtr
	rec.LastError = lastError
	rec.EndedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if status == "succeeded" {
		_ = logs.AppendEvent(m.stateDir, runID, logs.Event{Phase: "runtime.exit", Runtime: string(target), ContainerID: containerID, Message: "completed"})
		return rec, nil
	}
	_ = logs.AppendEvent(m.stateDir, runID, logs.Event{Phase: "runtime.exit", Runtime: string(target), ContainerID: containerID, Message: "failed", Error: lastError})
	if runErr != nil {
		return rec, runErr
	}
	return rec, fmt.Errorf("run failed with exit code %d", runRes.ExitCode)
}

func (m *Manager) ListRuns(limit int) ([]store.RunRecord, error) {
	recs, err := m.store.ListRuns(limit)
	if err != nil {
		return nil, err
	}
	for i := range recs {
		updated, refreshErr := m.refreshRunStatus(context.Background(), recs[i])
		if refreshErr == nil {
			recs[i] = updated
		}
	}
	return recs, nil
}

func (m *Manager) GetRun(runID string) (store.RunRecord, error) {
	rec, err := m.store.GetRun(runID)
	if err != nil {
		return store.RunRecord{}, err
	}
	updated, refreshErr := m.refreshRunStatus(context.Background(), rec)
	if refreshErr != nil {
		return rec, nil
	}
	return updated, nil
}

func (m *Manager) ReadEvents(runID string) ([]string, error) {
	return logs.ReadEvents(m.stateDir, runID)
}

func (m *Manager) RuntimeLogs(ctx context.Context, r store.RunRecord, follow bool) (string, error) {
	t, err := runtime.ParseTarget(r.RuntimeTarget)
	if err != nil {
		return "", err
	}
	ad, ok := m.resolver.Adapter(t)
	if !ok {
		return "", fmt.Errorf("runtime adapter unavailable: %s", r.RuntimeTarget)
	}
	return ad.Logs(ctx, r.ContainerID, follow)
}

func (m *Manager) RuntimeInspect(ctx context.Context, r store.RunRecord) (string, error) {
	t, err := runtime.ParseTarget(r.RuntimeTarget)
	if err != nil {
		return "", err
	}
	ad, ok := m.resolver.Adapter(t)
	if !ok {
		return "", fmt.Errorf("runtime adapter unavailable: %s", r.RuntimeTarget)
	}
	return ad.Inspect(ctx, r.ContainerID)
}

func (m *Manager) DebugShell(ctx context.Context, runID string) error {
	r, err := m.store.GetRun(runID)
	if err != nil {
		return err
	}
	if r.Status != "failed_paused" && r.Status != "running" {
		return fmt.Errorf("run %s is not debuggable (status=%s)", runID, r.Status)
	}
	t, err := runtime.ParseTarget(r.RuntimeTarget)
	if err != nil {
		return err
	}
	ad, ok := m.resolver.Adapter(t)
	if !ok {
		return fmt.Errorf("runtime adapter unavailable: %s", r.RuntimeTarget)
	}
	return ad.ExecShell(ctx, r.ContainerID)
}

func (m *Manager) prepareCapsule(inputPath string) (v1.Clawfile, policy.Policy, string, string, error) {
	st, err := os.Stat(inputPath)
	if err != nil {
		return v1.Clawfile{}, policy.Policy{}, "", "", err
	}
	if !st.IsDir() && strings.HasSuffix(inputPath, ".claw") {
		outDir := filepath.Join(m.stateDir, "capsules")
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			return v1.Clawfile{}, policy.Policy{}, "", "", err
		}
		res, err := compiler.Compile(inputPath, outDir)
		if err != nil {
			return v1.Clawfile{}, policy.Policy{}, "", "", err
		}
		return res.Config, res.Policy, res.Capsule.Path, res.Capsule.ID, nil
	}
	if st.IsDir() {
		return loadFromCapsuleDir(inputPath)
	}
	return v1.Clawfile{}, policy.Policy{}, "", "", fmt.Errorf("input must be .claw file or capsule directory")
}

func loadFromCapsuleDir(capPath string) (v1.Clawfile, policy.Policy, string, string, error) {
	m, err := capsule.Load(capPath)
	if err != nil {
		return v1.Clawfile{}, policy.Policy{}, "", "", fmt.Errorf("load capsule manifest: %w", err)
	}
	irBytes, err := os.ReadFile(filepath.Join(capPath, "ir.json"))
	if err != nil {
		return v1.Clawfile{}, policy.Policy{}, "", "", err
	}
	var ir struct {
		Clawfile v1.Clawfile `json:"clawfile"`
	}
	if err := json.Unmarshal(irBytes, &ir); err != nil {
		return v1.Clawfile{}, policy.Policy{}, "", "", fmt.Errorf("parse capsule ir: %w", err)
	}
	pBytes, err := os.ReadFile(filepath.Join(capPath, "policy.json"))
	if err != nil {
		return v1.Clawfile{}, policy.Policy{}, "", "", err
	}
	var pol policy.Policy
	if err := json.Unmarshal(pBytes, &pol); err != nil {
		return v1.Clawfile{}, policy.Policy{}, "", "", fmt.Errorf("parse capsule policy: %w", err)
	}
	return ir.Clawfile, pol, capPath, m.CapsuleID, nil
}

func makeRunID() string {
	now := time.Now().UTC()
	return now.Format("20060102t150405") + fmt.Sprintf("%09d", now.Nanosecond())
}

func writeRunOutput(stateDir, runID, fileName, content string) error {
	path := filepath.Join(stateDir, "runs", runID, fileName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func intPtr(v int) *int { return &v }

func mergeEnv(maps ...map[string]string) map[string]string {
	return mergeEnvMany(maps...)
}

func mergeEnvMany(maps ...map[string]string) map[string]string {
	total := 0
	for _, m := range maps {
		total += len(m)
	}
	out := make(map[string]string, total)
	for _, m := range maps {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

func filterEnvAllowlist(env map[string]string, allow map[string]struct{}) map[string]string {
	if len(env) == 0 || len(allow) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(env))
	for k, v := range env {
		if _, ok := allow[k]; ok {
			out[k] = v
		}
	}
	return out
}

var envNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func resolveHostSecretEnvs(names []string) (map[string]string, error) {
	if len(names) == 0 {
		return map[string]string{}, nil
	}
	normalized := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		if !envNameRe.MatchString(name) {
			return nil, fmt.Errorf("invalid --secret-env name: %q", raw)
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		normalized = append(normalized, name)
	}
	sort.Strings(normalized)

	out := make(map[string]string, len(normalized))
	for _, name := range normalized {
		value := os.Getenv(name)
		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("host env %s is empty", name)
		}
		out[name] = value
	}
	return out, nil
}

func (m *Manager) refreshRunStatus(ctx context.Context, rec store.RunRecord) (store.RunRecord, error) {
	if rec.Status != "running" || rec.ContainerID == "" {
		return rec, nil
	}
	target, err := runtime.ParseTarget(rec.RuntimeTarget)
	if err != nil {
		return rec, err
	}
	adapter, ok := m.resolver.Adapter(target)
	if !ok {
		return rec, fmt.Errorf("runtime adapter unavailable: %s", rec.RuntimeTarget)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
	}
	raw, err := adapter.Inspect(ctx, rec.ContainerID)
	if err != nil {
		return rec, err
	}
	containerStatus, exitCode, err := parseContainerInspectState(raw)
	if err != nil {
		return rec, err
	}
	runStatus, terminal := mapContainerStatus(containerStatus, exitCode)
	if !terminal {
		return rec, nil
	}
	lastError := ""
	if runStatus == "failed" {
		if exitCode != nil {
			lastError = fmt.Sprintf("detached container exited with code %d", *exitCode)
		} else {
			lastError = "detached container exited"
		}
	}
	if err := m.store.UpdateRunCompletion(rec.RunID, runStatus, rec.ContainerID, exitCode, lastError); err != nil {
		return rec, err
	}
	rec.Status = runStatus
	rec.ExitCode = exitCode
	rec.LastError = lastError
	rec.EndedAt = time.Now().UTC().Format(time.RFC3339Nano)
	message := "completed"
	if runStatus == "failed" {
		message = "failed"
	}
	_ = logs.AppendEvent(m.stateDir, rec.RunID, logs.Event{
		Phase:       "runtime.exit",
		Runtime:     rec.RuntimeTarget,
		ContainerID: rec.ContainerID,
		Message:     message,
		Error:       lastError,
	})
	return rec, nil
}

type inspectPayload struct {
	State      inspectState `json:"State"`
	StateLower inspectState `json:"state"`
}

type inspectState struct {
	Status        string `json:"Status"`
	StatusLower   string `json:"status"`
	ExitCode      *int   `json:"ExitCode"`
	ExitCodeLower *int   `json:"exitCode"`
}

func parseContainerInspectState(raw string) (string, *int, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil, fmt.Errorf("empty inspect payload")
	}
	if strings.HasPrefix(trimmed, "[") {
		var payload []inspectPayload
		if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
			return "", nil, err
		}
		if len(payload) == 0 {
			return "", nil, fmt.Errorf("inspect payload is empty")
		}
		return payload[0].normalize()
	}
	var payload inspectPayload
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return "", nil, err
	}
	return payload.normalize()
}

func mapContainerStatus(status string, exitCode *int) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "running", "created", "restarting", "paused":
		return "running", false
	case "exited", "dead", "stopped":
		if exitCode != nil && *exitCode == 0 {
			return "succeeded", true
		}
		return "failed", true
	default:
		return "", false
	}
}

func (p inspectPayload) normalize() (string, *int, error) {
	status := p.State.Status
	if status == "" {
		status = p.State.StatusLower
	}
	if status == "" {
		status = p.StateLower.Status
	}
	if status == "" {
		status = p.StateLower.StatusLower
	}
	if status == "" {
		return "", nil, fmt.Errorf("inspect payload missing container status")
	}
	exitCode := p.State.ExitCode
	if exitCode == nil {
		exitCode = p.State.ExitCodeLower
	}
	if exitCode == nil {
		exitCode = p.StateLower.ExitCode
	}
	if exitCode == nil {
		exitCode = p.StateLower.ExitCodeLower
	}
	return strings.ToLower(status), exitCode, nil
}
