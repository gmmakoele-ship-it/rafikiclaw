package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

const cliPinnedImage = "alpine:3.20@sha256:a4f4213abb84c497377b8544c81b3564f313746700372ec4fe84653e4fb03805"

func TestRunKeygenReleaseVerify(t *testing.T) {
	root := t.TempDir()
	priv := filepath.Join(root, "k.priv.pem")
	pub := filepath.Join(root, "k.pub.pem")

	if code := runKeygen([]string{"--private-key", priv, "--public-key", pub}); code != 0 {
		t.Fatalf("runKeygen code=%d", code)
	}

	vault := filepath.Join(root, "vault")
	if err := os.MkdirAll(vault, 0o755); err != nil {
		t.Fatalf("mkdir vault: %v", err)
	}
	claw := filepath.Join(root, "agent.claw")
	if err := os.WriteFile(claw, []byte(renderCLIClaw(vault, "outbound")), 0o644); err != nil {
		t.Fatalf("write claw: %v", err)
	}

	out := filepath.Join(root, "out")
	if code := runRelease([]string{claw, "--out", out, "--strict", "--sign-key", priv}); code != 0 {
		t.Fatalf("runRelease code=%d", code)
	}

	entries, err := filepath.Glob(filepath.Join(out, "rel_*"))
	if err != nil {
		t.Fatalf("glob release dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one release in out dir, got %d", len(entries))
	}
	if code := runVerify([]string{entries[0], "--public-key", pub, "--require-release"}); code != 0 {
		t.Fatalf("runVerify code=%d", code)
	}
}

func TestRunReleaseStrictRejectsNetworkAll(t *testing.T) {
	root := t.TempDir()
	priv := filepath.Join(root, "k.priv.pem")
	pub := filepath.Join(root, "k.pub.pem")
	if code := runKeygen([]string{"--private-key", priv, "--public-key", pub}); code != 0 {
		t.Fatalf("runKeygen code=%d", code)
	}

	vault := filepath.Join(root, "vault")
	if err := os.MkdirAll(vault, 0o755); err != nil {
		t.Fatalf("mkdir vault: %v", err)
	}
	claw := filepath.Join(root, "agent.claw")
	if err := os.WriteFile(claw, []byte(renderCLIClaw(vault, "all")), 0o644); err != nil {
		t.Fatalf("write claw: %v", err)
	}

	out := filepath.Join(root, "out")
	if code := runRelease([]string{claw, "--out", out, "--strict", "--sign-key", priv}); code == 0 {
		t.Fatal("expected strict release failure")
	}
}

func renderCLIClaw(vaultPath, networkMode string) string {
	return fmt.Sprintf(`apiVersion: rafikiclaw/v1
kind: Agent
agent:
  name: cli-release-test
  species: nano
  lifecycle: ephemeral
  habitat:
    network:
      mode: %s
    mounts:
      - source: %s
        target: /vault
        readOnly: true
    env: {}
  runtime:
    image: %s
  command:
    - sh
    - -lc
    - echo "ok"
`, networkMode, vaultPath, cliPinnedImage)
}
