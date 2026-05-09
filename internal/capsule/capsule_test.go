package capsule

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gmmakoele-ship-it/rafikiclaw/internal/locks"
	"github.com/gmmakoele-ship-it/rafikiclaw/internal/policy"
)

func TestLoadVerifiesManifestAndPayloadDigests(t *testing.T) {
	root := t.TempDir()
	lk := locks.BundleLocks{
		Deps: locks.DepsLock{
			Version: "rafikiclaw.depslock/v1",
			Skills:  []locks.SkillLock{},
		},
		Image: locks.ImageLock{
			Version: "rafikiclaw.imagelock/v1",
			Image:   "alpine@sha256:test",
			Digest:  "sha256:test",
		},
		Source: locks.SourceLock{
			Version: "rafikiclaw.sourcelock/v1",
			Files:   []locks.FileHash{},
		},
	}
	pol := policy.Policy{
		Version: "rafikiclaw.policy/v1",
		Network: policy.NetworkPolicy{Mode: "none", Allowed: false},
	}
	cap, err := Write(root, "agent.claw", map[string]any{"hello": "world"}, pol, lk)
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	if _, err := Load(cap.Path); err != nil {
		t.Fatalf("Load() should succeed before tamper: %v", err)
	}

	irPath := filepath.Join(cap.Path, "ir.json")
	if err := os.WriteFile(irPath, []byte("{\"hello\":\"tampered\"}\n"), 0o644); err != nil {
		t.Fatalf("tamper ir.json: %v", err)
	}
	_, err = Load(cap.Path)
	if err == nil {
		t.Fatal("expected digest mismatch error")
	}
	if !strings.Contains(err.Error(), "capsule digest mismatch for ir") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadRejectsPathTraversalInLockManifest(t *testing.T) {
	root := t.TempDir()
	lk := locks.BundleLocks{
		Deps: locks.DepsLock{
			Version: "rafikiclaw.depslock/v1",
			Skills:  []locks.SkillLock{},
		},
		Image: locks.ImageLock{
			Version: "rafikiclaw.imagelock/v1",
			Image:   "alpine@sha256:test",
			Digest:  "sha256:test",
		},
		Source: locks.SourceLock{
			Version: "rafikiclaw.sourcelock/v1",
			Files:   []locks.FileHash{},
		},
	}
	pol := policy.Policy{
		Version: "rafikiclaw.policy/v1",
		Network: policy.NetworkPolicy{Mode: "none", Allowed: false},
	}
	cap, err := Write(root, "agent.claw", map[string]any{"hello": "world"}, pol, lk)
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	manifestPath := filepath.Join(cap.Path, "manifest.json")
	b, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	manifestText := string(b)
	manifestText = strings.Replace(manifestText, "locks/source.lock.json", "../source.lock.json", 1)
	if err := os.WriteFile(manifestPath, []byte(manifestText), 0o644); err != nil {
		t.Fatalf("rewrite manifest: %v", err)
	}

	_, err = Load(cap.Path)
	if err == nil {
		t.Fatal("expected invalid path error")
	}
	if !strings.Contains(err.Error(), "path escapes capsule root") {
		t.Fatalf("unexpected error: %v", err)
	}
}
