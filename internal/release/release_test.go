package release

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateAndVerifyReleaseStrict(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	clawPath := filepath.Join(root, "agent.claw")
	writeTestClaw(t, clawPath, "none")

	stateDir := filepath.Join(root, "state")
	res, err := Create(CreateOptions{
		InputPath: clawPath,
		StateDir:  stateDir,
		Strict:    true,
	})
	if err != nil {
		t.Fatalf("create release: %v", err)
	}
	if res.ReleaseDir == "" {
		t.Fatalf("expected release dir")
	}
	if res.CapsuleID == "" {
		t.Fatalf("expected capsule id")
	}
	if _, err := os.Stat(filepath.Join(res.ReleaseDir, "release.json")); err != nil {
		t.Fatalf("release manifest missing: %v", err)
	}

	verifyRes, err := Verify(VerifyOptions{InputPath: res.ReleaseDir, RequireRelease: true})
	if err != nil {
		t.Fatalf("verify release: %v", err)
	}
	if !verifyRes.Verified {
		t.Fatalf("expected verified=true")
	}
	if !verifyRes.SignatureValid {
		t.Fatalf("expected signature_valid=true")
	}
	if !verifyRes.StrictSatisfied {
		t.Fatalf("expected strict checks satisfied")
	}
}

func TestVerifyReleaseFailsAfterSignatureTamper(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	clawPath := filepath.Join(root, "agent.claw")
	writeTestClaw(t, clawPath, "none")

	res, err := Create(CreateOptions{
		InputPath: clawPath,
		StateDir:  filepath.Join(root, "state"),
		Strict:    true,
	})
	if err != nil {
		t.Fatalf("create release: %v", err)
	}

	sigPath := filepath.Join(res.ReleaseDir, "signing", "attestation.sig")
	if err := os.WriteFile(sigPath, []byte("ZmFrZV9zaWduYXR1cmU="), 0o644); err != nil {
		t.Fatalf("tamper signature: %v", err)
	}

	_, err = Verify(VerifyOptions{InputPath: res.ReleaseDir, RequireRelease: true})
	if err == nil {
		t.Fatalf("expected verify to fail after tamper")
	}
	if !strings.Contains(err.Error(), "signature verification failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateStrictRejectsNetworkAll(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	clawPath := filepath.Join(root, "agent.claw")
	writeTestClaw(t, clawPath, "all")

	_, err := Create(CreateOptions{
		InputPath: clawPath,
		StateDir:  filepath.Join(root, "state"),
		Strict:    true,
	})
	if err == nil {
		t.Fatalf("expected strict release to fail")
	}
	if !strings.Contains(err.Error(), "habitat.network_not_all") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestVerifyCapsuleDirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	clawPath := filepath.Join(root, "agent.claw")
	writeTestClaw(t, clawPath, "none")

	res, err := Create(CreateOptions{
		InputPath: clawPath,
		StateDir:  filepath.Join(root, "state"),
		Strict:    false,
	})
	if err != nil {
		t.Fatalf("create release: %v", err)
	}

	capsulePath := filepath.Join(res.ReleaseDir, "capsule")
	verifyRes, err := Verify(VerifyOptions{InputPath: capsulePath})
	if err != nil {
		t.Fatalf("verify capsule: %v", err)
	}
	if verifyRes.Kind != "capsule" {
		t.Fatalf("unexpected kind: %s", verifyRes.Kind)
	}
	if !verifyRes.Verified {
		t.Fatalf("expected verified")
	}
}

func writeTestClaw(t *testing.T, outPath string, networkMode string) {
	t.Helper()
	content := "apiVersion: rafikiclaw/v1\n" +
		"kind: Agent\n" +
		"agent:\n" +
		"  name: test-agent\n" +
		"  species: nano\n" +
		"  lifecycle: ephemeral\n" +
		"  habitat:\n" +
		"    network:\n" +
		"      mode: " + networkMode + "\n" +
		"    mounts: []\n" +
		"    env: {}\n" +
		"  runtime: {}\n" +
		"  command:\n" +
		"    - sh\n" +
		"    - -lc\n" +
		"    - echo test\n"
	if err := os.WriteFile(outPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write claw: %v", err)
	}
}
