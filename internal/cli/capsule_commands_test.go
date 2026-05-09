package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDiscoverCapsulesAndFilter(t *testing.T) {
	root := t.TempDir()
	capsuleRoot := filepath.Join(root, "capsules")
	if err := os.MkdirAll(capsuleRoot, 0o755); err != nil {
		t.Fatalf("mkdir capsule root: %v", err)
	}

	first := filepath.Join(capsuleRoot, "cap_1111111111111111")
	second := filepath.Join(capsuleRoot, "cap_2222222222222222")
	writeTestCapsule(t, first, "1111111111111111", "alpha")
	writeTestCapsule(t, second, "2222222222222222", "beta")

	now := time.Now().UTC()
	if err := os.Chtimes(first, now.Add(-2*time.Hour), now.Add(-2*time.Hour)); err != nil {
		t.Fatalf("chtimes first: %v", err)
	}
	if err := os.Chtimes(second, now.Add(-1*time.Hour), now.Add(-1*time.Hour)); err != nil {
		t.Fatalf("chtimes second: %v", err)
	}

	items, err := discoverCapsules(capsuleRoot)
	if err != nil {
		t.Fatalf("discoverCapsules() error = %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].ID != "2222222222222222" {
		t.Fatalf("expected newest capsule first, got %s", items[0].ID)
	}

	filtered := filterCapsules(items, "alp", false, time.Time{}, false, time.Time{})
	if len(filtered) != 1 || filtered[0].AgentName != "alpha" {
		t.Fatalf("expected agent filter to return alpha, got %+v", filtered)
	}
}

func TestParseTimeFilter(t *testing.T) {
	since, ok, err := parseTimeFilter("2026-02-06", false)
	if err != nil || !ok {
		t.Fatalf("parse since failed: ok=%v err=%v", ok, err)
	}
	if since.Hour() != 0 || since.Minute() != 0 || since.Second() != 0 {
		t.Fatalf("expected start-of-day for since date, got %s", since.Format(time.RFC3339))
	}

	until, ok, err := parseTimeFilter("2026-02-06", true)
	if err != nil || !ok {
		t.Fatalf("parse until failed: ok=%v err=%v", ok, err)
	}
	if until.Hour() != 23 || until.Minute() != 59 || until.Second() != 59 {
		t.Fatalf("expected end-of-day for until date, got %s", until.Format(time.RFC3339))
	}
}

func TestResolveCapsuleRefAndDiff(t *testing.T) {
	stateDir := t.TempDir()
	capsuleRoot := filepath.Join(stateDir, "capsules")
	if err := os.MkdirAll(capsuleRoot, 0o755); err != nil {
		t.Fatalf("mkdir capsule root: %v", err)
	}

	leftPath := filepath.Join(capsuleRoot, "cap_aaaa1111aaaa1111")
	rightPath := filepath.Join(capsuleRoot, "cap_bbbb2222bbbb2222")
	writeTestCapsule(t, leftPath, "aaaa1111aaaa1111", "alpha")
	writeTestCapsule(t, rightPath, "bbbb2222bbbb2222", "alpha")

	// Inject a meaningful policy difference for diff assertion.
	policyPath := filepath.Join(rightPath, "policy.json")
	policy := map[string]any{
		"version": "rafikiclaw.policy/v1",
		"network": map[string]any{"mode": "outbound", "allowed": true},
		"mounts":  []any{},
	}
	writeJSONFile(t, policyPath, policy)
	refreshCapsuleManifestDigests(t, rightPath)

	left, err := resolveCapsuleRef(stateDir, "aaaa1111")
	if err != nil {
		t.Fatalf("resolve left failed: %v", err)
	}
	right, err := resolveCapsuleRef(stateDir, "cap_bbbb2222bbbb2222")
	if err != nil {
		t.Fatalf("resolve right failed: %v", err)
	}

	res := diffCapsules(left, right)
	if res.Equal {
		t.Fatal("expected diff to detect section changes")
	}
	foundPolicyDiff := false
	for _, sec := range res.Sections {
		if sec.Section == "policy" && !sec.Equal {
			foundPolicyDiff = true
			break
		}
	}
	if !foundPolicyDiff {
		t.Fatalf("expected non-equal policy section, got %+v", res.Sections)
	}
}

func writeTestCapsule(t *testing.T, capPath string, id string, agentName string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(capPath, "locks"), 0o755); err != nil {
		t.Fatalf("mkdir capsule: %v", err)
	}
	manifest := map[string]any{
		"version":        "rafikiclaw.capsule/v1",
		"capsuleId":      id,
		"sourceClawfile": "agent.claw",
		"digests": map[string]any{
			"ir":     "",
			"policy": "",
			"deps":   "",
			"image":  "",
			"source": "",
		},
		"runtimeCompatibility": map[string]any{
			"targets":   []any{"docker"},
			"semantics": []any{"detach"},
		},
		"locks": map[string]any{
			"dependency": "locks/deps.lock.json",
			"image":      "locks/image.lock.json",
			"source":     "locks/source.lock.json",
		},
	}
	ir := map[string]any{
		"clawfile": map[string]any{
			"agent": map[string]any{
				"name": agentName,
			},
		},
	}
	policy := map[string]any{
		"version": "rafikiclaw.policy/v1",
		"network": map[string]any{"mode": "none", "allowed": false},
		"mounts":  []any{},
	}
	deps := map[string]any{"version": "rafikiclaw.depslock/v1", "skills": []any{}}
	image := map[string]any{"version": "rafikiclaw.imagelock/v1", "image": "alpine@sha256:test", "digest": "sha256:test"}
	source := map[string]any{"version": "rafikiclaw.sourcelock/v1", "files": []any{}}

	writeJSONFile(t, filepath.Join(capPath, "manifest.json"), manifest)
	writeJSONFile(t, filepath.Join(capPath, "ir.json"), ir)
	writeJSONFile(t, filepath.Join(capPath, "policy.json"), policy)
	writeJSONFile(t, filepath.Join(capPath, "locks", "deps.lock.json"), deps)
	writeJSONFile(t, filepath.Join(capPath, "locks", "image.lock.json"), image)
	writeJSONFile(t, filepath.Join(capPath, "locks", "source.lock.json"), source)
	refreshCapsuleManifestDigests(t, capPath)
}

func writeJSONFile(t *testing.T, path string, data any) {
	t.Helper()
	b, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal %s: %v", path, err)
	}
	if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func refreshCapsuleManifestDigests(t *testing.T, capPath string) {
	t.Helper()
	manifestPath := filepath.Join(capPath, "manifest.json")
	b, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(b, &manifest); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	digests := map[string]any{
		"ir":     fileDigest(t, filepath.Join(capPath, "ir.json")),
		"policy": fileDigest(t, filepath.Join(capPath, "policy.json")),
		"deps":   fileDigest(t, filepath.Join(capPath, "locks", "deps.lock.json")),
		"image":  fileDigest(t, filepath.Join(capPath, "locks", "image.lock.json")),
		"source": fileDigest(t, filepath.Join(capPath, "locks", "source.lock.json")),
	}
	manifest["digests"] = digests
	writeJSONFile(t, manifestPath, manifest)
}

func fileDigest(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func TestDiffJSONSectionArrayAndFieldChanges(t *testing.T) {
	left := map[string]any{
		"a":   float64(1),
		"b":   map[string]any{"c": float64(2)},
		"arr": []any{float64(1), float64(2)},
	}
	right := map[string]any{
		"a":   float64(1),
		"b":   map[string]any{"c": float64(3)},
		"arr": []any{float64(1)},
		"d":   true,
	}
	d := diffJSONSection("ir", left, right)
	if d.Equal {
		t.Fatal("expected section to differ")
	}

	foundChanged := false
	foundRemoved := false
	foundAdded := false
	for _, c := range d.Changed {
		if c.Path == "b.c" {
			foundChanged = true
		}
	}
	for _, c := range d.Removed {
		if strings.Contains(c.Path, "arr[1]") {
			foundRemoved = true
		}
	}
	for _, c := range d.Added {
		if c.Path == "d" {
			foundAdded = true
		}
	}
	if !foundChanged || !foundRemoved || !foundAdded {
		t.Fatalf("missing expected diff signals: changed=%v removed=%v added=%v", foundChanged, foundRemoved, foundAdded)
	}
}
