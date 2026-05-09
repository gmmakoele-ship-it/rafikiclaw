package capsule

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gmmakoele-ship-it/rafikiclaw/internal/locks"
	"github.com/gmmakoele-ship-it/rafikiclaw/internal/policy"
)

type Manifest struct {
	Version              string            `json:"version"`
	CapsuleID            string            `json:"capsuleId"`
	SourceClawfile       string            `json:"sourceClawfile"`
	Digests              map[string]string `json:"digests"`
	RuntimeCompatibility RuntimeContract   `json:"runtimeCompatibility"`
	Locks                LockManifest      `json:"locks"`
	Release              *ReleaseMetadata  `json:"release,omitempty"`
}

type RuntimeContract struct {
	Targets   []string `json:"targets"`
	Semantics []string `json:"semantics"`
}

type LockManifest struct {
	Dependency string `json:"dependency"`
	Image      string `json:"image"`
	Source     string `json:"source"`
}

type ReleaseMetadata struct {
	Profile    string            `json:"profile"`
	CreatedAt  string            `json:"createdAt"`
	Provenance ReleaseProvenance `json:"provenance"`
	Signature  ReleaseSignature  `json:"signature"`
}

type ReleaseProvenance struct {
	Version         string            `json:"version"`
	Builder         string            `json:"builder"`
	CreatedAt       string            `json:"createdAt"`
	Profile         string            `json:"profile"`
	SourceGitCommit string            `json:"sourceGitCommit,omitempty"`
	SourceGitTree   string            `json:"sourceGitTree,omitempty"`
	SourceFileCount int               `json:"sourceFileCount"`
	Digests         map[string]string `json:"digests"`
}

type ReleaseSignature struct {
	Version       string `json:"version"`
	Algorithm     string `json:"algorithm"`
	KeyID         string `json:"keyId"`
	PayloadDigest string `json:"payloadDigest"`
	Value         string `json:"value"`
}

type Capsule struct {
	ID   string
	Path string
	Manifest
}

func Write(outputDir string, sourceClawfile string, ir any, pol policy.Policy, lk locks.BundleLocks) (Capsule, error) {
	if outputDir == "" {
		outputDir = "."
	}

	irJSON, err := canonicalJSON(ir)
	if err != nil {
		return Capsule{}, fmt.Errorf("marshal ir: %w", err)
	}
	policyJSON, err := canonicalJSON(pol)
	if err != nil {
		return Capsule{}, fmt.Errorf("marshal policy: %w", err)
	}
	depsJSON, err := canonicalJSON(lk.Deps)
	if err != nil {
		return Capsule{}, fmt.Errorf("marshal deps lock: %w", err)
	}
	imageJSON, err := canonicalJSON(lk.Image)
	if err != nil {
		return Capsule{}, fmt.Errorf("marshal image lock: %w", err)
	}
	sourceJSON, err := canonicalJSON(lk.Source)
	if err != nil {
		return Capsule{}, fmt.Errorf("marshal source lock: %w", err)
	}

	digests := map[string]string{
		"ir":     digest(irJSON),
		"policy": digest(policyJSON),
		"deps":   digest(depsJSON),
		"image":  digest(imageJSON),
		"source": digest(sourceJSON),
	}
	capsuleID := makeCapsuleID(digests)

	manifest := Manifest{
		Version:        "rafikiclaw.capsule/v1",
		CapsuleID:      capsuleID,
		SourceClawfile: filepath.Base(sourceClawfile),
		Digests:        digests,
		RuntimeCompatibility: RuntimeContract{
			Targets:   []string{"podman", "apple_container", "docker"},
			Semantics: []string{"detach", "env", "volume", "workdir"},
		},
		Locks: LockManifest{
			Dependency: "locks/deps.lock.json",
			Image:      "locks/image.lock.json",
			Source:     "locks/source.lock.json",
		},
	}
	manifestJSON, err := canonicalJSON(manifest)
	if err != nil {
		return Capsule{}, fmt.Errorf("marshal manifest: %w", err)
	}

	capPath := filepath.Join(outputDir, "cap_"+capsuleID)
	if err := os.MkdirAll(filepath.Join(capPath, "locks"), 0o755); err != nil {
		return Capsule{}, fmt.Errorf("create capsule dir: %w", err)
	}
	if err := writeFile(filepath.Join(capPath, "manifest.json"), manifestJSON); err != nil {
		return Capsule{}, err
	}
	if err := writeFile(filepath.Join(capPath, "ir.json"), irJSON); err != nil {
		return Capsule{}, err
	}
	if err := writeFile(filepath.Join(capPath, "policy.json"), policyJSON); err != nil {
		return Capsule{}, err
	}
	if err := writeFile(filepath.Join(capPath, "locks", "deps.lock.json"), depsJSON); err != nil {
		return Capsule{}, err
	}
	if err := writeFile(filepath.Join(capPath, "locks", "image.lock.json"), imageJSON); err != nil {
		return Capsule{}, err
	}
	if err := writeFile(filepath.Join(capPath, "locks", "source.lock.json"), sourceJSON); err != nil {
		return Capsule{}, err
	}
	portable := map[string]any{
		"version": "rafikiclaw.portable/v1",
		"image":   lk.Image.Image,
		"network": pol.Network.Mode,
		"mounts":  pol.Mounts,
	}
	portableJSON, err := canonicalJSON(portable)
	if err != nil {
		return Capsule{}, fmt.Errorf("marshal portable spec: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(capPath, "compat"), 0o755); err != nil {
		return Capsule{}, err
	}
	if err := writeFile(filepath.Join(capPath, "compat", "portable-run-spec.json"), portableJSON); err != nil {
		return Capsule{}, err
	}

	return Capsule{ID: capsuleID, Path: capPath, Manifest: manifest}, nil
}

func Load(path string) (Manifest, error) {
	b, err := os.ReadFile(filepath.Join(path, "manifest.json"))
	if err != nil {
		return Manifest{}, err
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return Manifest{}, err
	}
	if err := verifyManifest(path, m); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

func writeFile(path string, b []byte) error {
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func canonicalJSON(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return json.MarshalIndent(out, "", "  ")
}

func digest(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func makeCapsuleID(digests map[string]string) string {
	keys := make([]string, 0, len(digests))
	for k := range digests {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, k := range keys {
		_, _ = h.Write([]byte(k))
		_, _ = h.Write([]byte(digests[k]))
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func verifyManifest(basePath string, m Manifest) error {
	if m.CapsuleID == "" {
		return fmt.Errorf("capsule manifest missing capsuleId")
	}
	required := map[string]string{
		"ir":     "ir.json",
		"policy": "policy.json",
		"deps":   m.Locks.Dependency,
		"image":  m.Locks.Image,
		"source": m.Locks.Source,
	}
	for key, relPath := range required {
		expected, ok := m.Digests[key]
		if !ok || expected == "" {
			return fmt.Errorf("capsule manifest missing digest for %s", key)
		}
		absPath, err := resolveCapsulePath(basePath, relPath)
		if err != nil {
			return fmt.Errorf("capsule manifest path for %s is invalid: %w", key, err)
		}
		b, err := os.ReadFile(absPath)
		if err != nil {
			return fmt.Errorf("read capsule %s: %w", relPath, err)
		}
		got := digest(b)
		if got != expected {
			return fmt.Errorf("capsule digest mismatch for %s: expected %s, got %s", key, expected, got)
		}
	}

	return nil
}

func resolveCapsulePath(basePath, relPath string) (string, error) {
	if relPath == "" {
		return "", fmt.Errorf("empty path")
	}
	if filepath.IsAbs(relPath) {
		return "", fmt.Errorf("absolute paths are not allowed")
	}
	clean := filepath.Clean(relPath)
	if clean == "." || strings.HasPrefix(clean, "..") {
		return "", fmt.Errorf("path escapes capsule root: %s", relPath)
	}
	absBase, err := filepath.Abs(basePath)
	if err != nil {
		return "", err
	}
	abs := filepath.Join(absBase, clean)
	rel, err := filepath.Rel(absBase, abs)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("path escapes capsule root: %s", relPath)
	}
	return abs, nil
}
