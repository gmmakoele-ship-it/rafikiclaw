package release

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"time"

	"github.com/gmmakoele-ship-it/rafikiclaw/internal/capsule"
	v1 "github.com/gmmakoele-ship-it/rafikiclaw/internal/claw/schema/v1"
	"github.com/gmmakoele-ship-it/rafikiclaw/internal/compiler"
	"github.com/gmmakoele-ship-it/rafikiclaw/internal/locks"
	"github.com/gmmakoele-ship-it/rafikiclaw/internal/policy"
)

type CreateOptions struct {
	InputPath      string
	StateDir       string
	OutputDir      string
	Strict         bool
	PrivateKeyPath string
	KeyID          string
}

type CreateResult struct {
	ReleaseDir      string
	ReleaseID       string
	CapsuleID       string
	CapsulePath     string
	CreatedCapsule  bool
	PrivateKeyPath  string
	PublicKeyPath   string
	Checks          []StrictCheck
	StrictEnforced  bool
	ReleaseManifest ReleaseManifest
}

type VerifyOptions struct {
	InputPath      string
	PublicKeyPath  string
	RequireRelease bool
}

type VerifyResult struct {
	Kind            string
	Verified        bool
	ReleaseID       string
	CapsuleID       string
	ReleasePath     string
	CapsulePath     string
	SignatureValid  bool
	StrictSatisfied bool
	Checks          []StrictCheck
}

type ReleaseManifest struct {
	Version   string           `json:"version"`
	ReleaseID string           `json:"releaseId"`
	CreatedAt string           `json:"createdAt"`
	Strict    bool             `json:"strict"`
	Capsule   ReleaseCapsule   `json:"capsule"`
	Artifacts ReleaseArtifacts `json:"artifacts"`
	Signing   ReleaseSigning   `json:"signing"`
	Checks    []StrictCheck    `json:"checks"`
}

type ReleaseCapsule struct {
	ID             string `json:"id"`
	Path           string `json:"path"`
	SourceClawfile string `json:"sourceClawfile"`
}

type ReleaseArtifacts struct {
	Provenance  string `json:"provenance"`
	Attestation string `json:"attestation"`
	Signature   string `json:"signature"`
}

type ReleaseSigning struct {
	Algorithm string `json:"algorithm"`
	KeyID     string `json:"keyId"`
	PublicKey string `json:"publicKey"`
}

type Attestation struct {
	Version   string            `json:"version"`
	ReleaseID string            `json:"releaseId"`
	CreatedAt string            `json:"createdAt"`
	CapsuleID string            `json:"capsuleId"`
	Strict    bool              `json:"strict"`
	KeyID     string            `json:"keyId"`
	Digests   map[string]string `json:"digests"`
}

type Provenance struct {
	Version        string `json:"version"`
	CreatedAt      string `json:"createdAt"`
	ToolModule     string `json:"toolModule"`
	ToolVersion    string `json:"toolVersion"`
	GoVersion      string `json:"goVersion"`
	HostOS         string `json:"hostOS"`
	HostArch       string `json:"hostArch"`
	SourceClawfile string `json:"sourceClawfile"`
	GitCommit      string `json:"gitCommit,omitempty"`
	GitTree        string `json:"gitTree,omitempty"`
	SourceFiles    int    `json:"sourceFiles"`
}

type StrictCheck struct {
	Name    string `json:"name"`
	Passed  bool   `json:"passed"`
	Details string `json:"details"`
}

type irDoc struct {
	Clawfile v1.Clawfile `json:"clawfile"`
	Runtime  struct {
		Image string `json:"image"`
	} `json:"runtime"`
}

func Create(opts CreateOptions) (CreateResult, error) {
	if strings.TrimSpace(opts.InputPath) == "" {
		return CreateResult{}, fmt.Errorf("input path is required")
	}
	stateDir := strings.TrimSpace(opts.StateDir)
	if stateDir == "" {
		stateDir = ".metaclaw"
	}
	outputDir := strings.TrimSpace(opts.OutputDir)
	if outputDir == "" {
		outputDir = filepath.Join(stateDir, "releases")
	}

	capsulePath, capID, createdCapsule, err := prepareCapsule(opts.InputPath, stateDir)
	if err != nil {
		return CreateResult{}, err
	}

	manifest, err := capsule.Load(capsulePath)
	if err != nil {
		return CreateResult{}, fmt.Errorf("load capsule: %w", err)
	}
	if capID != "" && manifest.CapsuleID != capID {
		return CreateResult{}, fmt.Errorf("capsule id mismatch after compile: expected %s, got %s", capID, manifest.CapsuleID)
	}

	ir, pol, srcLock, err := loadCapsuleDocs(capsulePath)
	if err != nil {
		return CreateResult{}, err
	}

	checks := strictChecks(ir, pol, srcLock)
	if opts.Strict {
		if failed := failedChecks(checks); len(failed) > 0 {
			return CreateResult{}, fmt.Errorf("strict checks failed: %s", strings.Join(failed, "; "))
		}
	}

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return CreateResult{}, fmt.Errorf("create output dir: %w", err)
	}

	releaseID := makeReleaseID(manifest.CapsuleID)
	releaseDir := filepath.Join(outputDir, "rel_"+releaseID)
	if err := os.MkdirAll(releaseDir, 0o755); err != nil {
		return CreateResult{}, fmt.Errorf("create release dir: %w", err)
	}

	releaseCapsulePath := filepath.Join(releaseDir, "capsule")
	if err := copyDir(capsulePath, releaseCapsulePath); err != nil {
		return CreateResult{}, fmt.Errorf("copy capsule: %w", err)
	}

	privateKeyPath := strings.TrimSpace(opts.PrivateKeyPath)
	if privateKeyPath == "" {
		privateKeyPath = filepath.Join(stateDir, "keys", "release_ed25519.pem")
	}
	priv, pub, createdKey, err := loadOrCreatePrivateKey(privateKeyPath)
	if err != nil {
		return CreateResult{}, fmt.Errorf("load signing key: %w", err)
	}
	if createdKey {
		if err := os.Chmod(privateKeyPath, 0o600); err != nil {
			return CreateResult{}, fmt.Errorf("set key permissions: %w", err)
		}
	}

	keyID := strings.TrimSpace(opts.KeyID)
	if keyID == "" {
		keyID = deriveKeyID(pub)
	}

	publicKeyRel := filepath.Join("signing", "public_key.pem")
	publicKeyPath := filepath.Join(releaseDir, publicKeyRel)
	if err := os.MkdirAll(filepath.Dir(publicKeyPath), 0o755); err != nil {
		return CreateResult{}, fmt.Errorf("create signing dir: %w", err)
	}
	if err := writePublicKeyPEM(publicKeyPath, pub); err != nil {
		return CreateResult{}, fmt.Errorf("write public key: %w", err)
	}

	createdAt := time.Now().UTC().Format(time.RFC3339Nano)
	releaseManifest := ReleaseManifest{
		Version:   "metaclaw.release/v1",
		ReleaseID: releaseID,
		CreatedAt: createdAt,
		Strict:    opts.Strict,
		Capsule: ReleaseCapsule{
			ID:             manifest.CapsuleID,
			Path:           "capsule",
			SourceClawfile: manifest.SourceClawfile,
		},
		Artifacts: ReleaseArtifacts{
			Provenance:  "provenance.json",
			Attestation: "attestation.json",
			Signature:   filepath.Join("signing", "attestation.sig"),
		},
		Signing: ReleaseSigning{
			Algorithm: "ed25519",
			KeyID:     keyID,
			PublicKey: publicKeyRel,
		},
		Checks: checks,
	}

	releaseJSON, err := canonicalJSON(releaseManifest)
	if err != nil {
		return CreateResult{}, fmt.Errorf("marshal release manifest: %w", err)
	}
	releaseManifestPath := filepath.Join(releaseDir, "release.json")
	if err := os.WriteFile(releaseManifestPath, releaseJSON, 0o644); err != nil {
		return CreateResult{}, fmt.Errorf("write release manifest: %w", err)
	}

	prov := buildProvenance(createdAt, manifest, srcLock)
	provJSON, err := canonicalJSON(prov)
	if err != nil {
		return CreateResult{}, fmt.Errorf("marshal provenance: %w", err)
	}
	provenancePath := filepath.Join(releaseDir, releaseManifest.Artifacts.Provenance)
	if err := os.WriteFile(provenancePath, provJSON, 0o644); err != nil {
		return CreateResult{}, fmt.Errorf("write provenance: %w", err)
	}

	capsuleManifestPath := filepath.Join(releaseCapsulePath, "manifest.json")
	capsuleManifestJSON, err := os.ReadFile(capsuleManifestPath)
	if err != nil {
		return CreateResult{}, fmt.Errorf("read capsule manifest: %w", err)
	}

	att := Attestation{
		Version:   "metaclaw.attestation/v1",
		ReleaseID: releaseID,
		CreatedAt: createdAt,
		CapsuleID: manifest.CapsuleID,
		Strict:    opts.Strict,
		KeyID:     keyID,
		Digests: map[string]string{
			"release":          digest(releaseJSON),
			"provenance":       digest(provJSON),
			"capsule_manifest": digest(capsuleManifestJSON),
		},
	}
	attJSON, err := canonicalJSON(att)
	if err != nil {
		return CreateResult{}, fmt.Errorf("marshal attestation: %w", err)
	}
	attestationPath := filepath.Join(releaseDir, releaseManifest.Artifacts.Attestation)
	if err := os.WriteFile(attestationPath, attJSON, 0o644); err != nil {
		return CreateResult{}, fmt.Errorf("write attestation: %w", err)
	}

	sig := ed25519.Sign(priv, attJSON)
	sigPath := filepath.Join(releaseDir, releaseManifest.Artifacts.Signature)
	if err := os.MkdirAll(filepath.Dir(sigPath), 0o755); err != nil {
		return CreateResult{}, fmt.Errorf("create signature dir: %w", err)
	}
	if err := os.WriteFile(sigPath, []byte(base64.StdEncoding.EncodeToString(sig)), 0o644); err != nil {
		return CreateResult{}, fmt.Errorf("write signature: %w", err)
	}

	return CreateResult{
		ReleaseDir:      releaseDir,
		ReleaseID:       releaseID,
		CapsuleID:       manifest.CapsuleID,
		CapsulePath:     releaseCapsulePath,
		CreatedCapsule:  createdCapsule,
		PrivateKeyPath:  privateKeyPath,
		PublicKeyPath:   publicKeyPath,
		Checks:          checks,
		StrictEnforced:  opts.Strict,
		ReleaseManifest: releaseManifest,
	}, nil
}

func Verify(opts VerifyOptions) (VerifyResult, error) {
	if strings.TrimSpace(opts.InputPath) == "" {
		return VerifyResult{}, fmt.Errorf("input path is required")
	}
	st, err := os.Stat(opts.InputPath)
	if err != nil {
		return VerifyResult{}, err
	}
	if !st.IsDir() {
		return VerifyResult{}, fmt.Errorf("input must be a directory")
	}

	releasePath := filepath.Join(opts.InputPath, "release.json")
	if _, err := os.Stat(releasePath); err == nil {
		return verifyReleaseDir(opts)
	}
	if opts.RequireRelease {
		return VerifyResult{}, fmt.Errorf("release manifest not found: %s", releasePath)
	}

	manifest, err := capsule.Load(opts.InputPath)
	if err != nil {
		return VerifyResult{}, fmt.Errorf("capsule verify failed: %w", err)
	}
	return VerifyResult{
		Kind:           "capsule",
		Verified:       true,
		CapsuleID:      manifest.CapsuleID,
		CapsulePath:    opts.InputPath,
		SignatureValid: false,
		Checks: []StrictCheck{{
			Name:    "capsule.digest_integrity",
			Passed:  true,
			Details: "manifest and artifact digests verified",
		}},
	}, nil
}

func verifyReleaseDir(opts VerifyOptions) (VerifyResult, error) {
	releaseRoot := opts.InputPath
	releaseJSON, err := os.ReadFile(filepath.Join(releaseRoot, "release.json"))
	if err != nil {
		return VerifyResult{}, fmt.Errorf("read release manifest: %w", err)
	}
	var rel ReleaseManifest
	if err := json.Unmarshal(releaseJSON, &rel); err != nil {
		return VerifyResult{}, fmt.Errorf("parse release manifest: %w", err)
	}

	capsulePath := filepath.Join(releaseRoot, rel.Capsule.Path)
	manifest, err := capsule.Load(capsulePath)
	if err != nil {
		return VerifyResult{}, fmt.Errorf("capsule verify failed: %w", err)
	}
	if manifest.CapsuleID != rel.Capsule.ID {
		return VerifyResult{}, fmt.Errorf("capsule id mismatch: release=%s capsule=%s", rel.Capsule.ID, manifest.CapsuleID)
	}

	provPath := filepath.Join(releaseRoot, rel.Artifacts.Provenance)
	attPath := filepath.Join(releaseRoot, rel.Artifacts.Attestation)
	sigPath := filepath.Join(releaseRoot, rel.Artifacts.Signature)

	provJSON, err := os.ReadFile(provPath)
	if err != nil {
		return VerifyResult{}, fmt.Errorf("read provenance: %w", err)
	}
	attJSON, err := os.ReadFile(attPath)
	if err != nil {
		return VerifyResult{}, fmt.Errorf("read attestation: %w", err)
	}
	sigRaw, err := os.ReadFile(sigPath)
	if err != nil {
		return VerifyResult{}, fmt.Errorf("read signature: %w", err)
	}

	var att Attestation
	if err := json.Unmarshal(attJSON, &att); err != nil {
		return VerifyResult{}, fmt.Errorf("parse attestation: %w", err)
	}
	if att.CapsuleID != manifest.CapsuleID {
		return VerifyResult{}, fmt.Errorf("attestation capsule id mismatch: %s != %s", att.CapsuleID, manifest.CapsuleID)
	}
	if att.ReleaseID != rel.ReleaseID {
		return VerifyResult{}, fmt.Errorf("attestation release id mismatch: %s != %s", att.ReleaseID, rel.ReleaseID)
	}
	if att.Strict != rel.Strict {
		return VerifyResult{}, fmt.Errorf("attestation strict mismatch")
	}
	if rel.Signing.KeyID != "" && att.KeyID != rel.Signing.KeyID {
		return VerifyResult{}, fmt.Errorf("attestation key id mismatch: release=%s attestation=%s", rel.Signing.KeyID, att.KeyID)
	}
	if got := att.Digests["release"]; got != digest(releaseJSON) {
		return VerifyResult{}, fmt.Errorf("release digest mismatch")
	}
	if got := att.Digests["provenance"]; got != digest(provJSON) {
		return VerifyResult{}, fmt.Errorf("provenance digest mismatch")
	}
	capManifestPath := filepath.Join(capsulePath, "manifest.json")
	capManifestJSON, err := os.ReadFile(capManifestPath)
	if err != nil {
		return VerifyResult{}, fmt.Errorf("read capsule manifest: %w", err)
	}
	if got := att.Digests["capsule_manifest"]; got != digest(capManifestJSON) {
		return VerifyResult{}, fmt.Errorf("capsule manifest digest mismatch")
	}

	sigData, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigRaw)))
	if err != nil {
		return VerifyResult{}, fmt.Errorf("decode signature: %w", err)
	}

	publicKeyPath := strings.TrimSpace(opts.PublicKeyPath)
	if publicKeyPath == "" {
		publicKeyPath = filepath.Join(releaseRoot, rel.Signing.PublicKey)
	}
	pub, err := loadPublicKey(publicKeyPath)
	if err != nil {
		return VerifyResult{}, fmt.Errorf("load public key: %w", err)
	}

	attCanonical, err := canonicalJSON(att)
	if err != nil {
		return VerifyResult{}, fmt.Errorf("canonicalize attestation: %w", err)
	}
	if !ed25519.Verify(pub, attCanonical, sigData) {
		return VerifyResult{}, fmt.Errorf("signature verification failed")
	}

	ir, pol, srcLock, err := loadCapsuleDocs(capsulePath)
	if err != nil {
		return VerifyResult{}, err
	}
	checks := strictChecks(ir, pol, srcLock)
	if rel.Strict {
		if failed := failedChecks(checks); len(failed) > 0 {
			return VerifyResult{}, fmt.Errorf("strict checks no longer satisfied: %s", strings.Join(failed, "; "))
		}
	}

	return VerifyResult{
		Kind:            "release",
		Verified:        true,
		ReleaseID:       rel.ReleaseID,
		CapsuleID:       manifest.CapsuleID,
		ReleasePath:     releaseRoot,
		CapsulePath:     capsulePath,
		SignatureValid:  true,
		StrictSatisfied: !rel.Strict || len(failedChecks(checks)) == 0,
		Checks:          checks,
	}, nil
}

func prepareCapsule(inputPath, stateDir string) (capsulePath string, capsuleID string, created bool, err error) {
	st, err := os.Stat(inputPath)
	if err != nil {
		return "", "", false, err
	}
	if st.IsDir() {
		m, err := capsule.Load(inputPath)
		if err != nil {
			return "", "", false, fmt.Errorf("load capsule: %w", err)
		}
		return inputPath, m.CapsuleID, false, nil
	}
	if strings.HasSuffix(strings.ToLower(inputPath), ".claw") {
		capsuleRoot := filepath.Join(stateDir, "capsules")
		if err := os.MkdirAll(capsuleRoot, 0o755); err != nil {
			return "", "", false, err
		}
		res, err := compiler.Compile(inputPath, capsuleRoot)
		if err != nil {
			return "", "", false, err
		}
		return res.Capsule.Path, res.Capsule.ID, true, nil
	}
	return "", "", false, fmt.Errorf("input must be .claw file or capsule directory")
}

func loadCapsuleDocs(capsulePath string) (irDoc, policy.Policy, locks.SourceLock, error) {
	irBytes, err := os.ReadFile(filepath.Join(capsulePath, "ir.json"))
	if err != nil {
		return irDoc{}, policy.Policy{}, locks.SourceLock{}, fmt.Errorf("read ir: %w", err)
	}
	var ir irDoc
	if err := json.Unmarshal(irBytes, &ir); err != nil {
		return irDoc{}, policy.Policy{}, locks.SourceLock{}, fmt.Errorf("parse ir: %w", err)
	}

	polBytes, err := os.ReadFile(filepath.Join(capsulePath, "policy.json"))
	if err != nil {
		return irDoc{}, policy.Policy{}, locks.SourceLock{}, fmt.Errorf("read policy: %w", err)
	}
	var pol policy.Policy
	if err := json.Unmarshal(polBytes, &pol); err != nil {
		return irDoc{}, policy.Policy{}, locks.SourceLock{}, fmt.Errorf("parse policy: %w", err)
	}

	srcBytes, err := os.ReadFile(filepath.Join(capsulePath, "locks", "source.lock.json"))
	if err != nil {
		return irDoc{}, policy.Policy{}, locks.SourceLock{}, fmt.Errorf("read source lock: %w", err)
	}
	var srcLock locks.SourceLock
	if err := json.Unmarshal(srcBytes, &srcLock); err != nil {
		return irDoc{}, policy.Policy{}, locks.SourceLock{}, fmt.Errorf("parse source lock: %w", err)
	}
	return ir, pol, srcLock, nil
}

func strictChecks(ir irDoc, pol policy.Policy, src locks.SourceLock) []StrictCheck {
	checks := make([]StrictCheck, 0, 8)

	image := strings.TrimSpace(ir.Clawfile.Agent.Runtime.Image)
	if image == "" {
		image = strings.TrimSpace(ir.Runtime.Image)
	}
	checks = append(checks, StrictCheck{
		Name:    "runtime.image_digest_pinned",
		Passed:  strings.Contains(image, "@sha256:"),
		Details: "runtime.image must be digest-pinned",
	})

	checks = append(checks, StrictCheck{
		Name:    "habitat.network_not_all",
		Passed:  strings.TrimSpace(pol.Network.Mode) != "all",
		Details: "strict mode forbids network=all",
	})

	mountSourceOK := true
	mountTargetOK := true
	mountTargetClean := true
	for _, m := range pol.Mounts {
		if !filepath.IsAbs(strings.TrimSpace(m.Source)) {
			mountSourceOK = false
		}
		target := strings.TrimSpace(m.Target)
		if !strings.HasPrefix(target, "/") {
			mountTargetOK = false
		}
		if path.Clean(target) != target {
			mountTargetClean = false
		}
	}
	checks = append(checks, StrictCheck{
		Name:    "habitat.mount_sources_absolute",
		Passed:  mountSourceOK,
		Details: "all mount sources must be absolute host paths",
	})
	checks = append(checks, StrictCheck{
		Name:    "habitat.mount_targets_absolute",
		Passed:  mountTargetOK,
		Details: "all mount targets must be absolute container paths",
	})
	checks = append(checks, StrictCheck{
		Name:    "habitat.mount_targets_clean",
		Passed:  mountTargetClean,
		Details: "mount targets must be normalized paths",
	})

	sourceNonEmpty := len(src.Files) > 0
	sourceRelOK := true
	for _, f := range src.Files {
		rel := filepath.ToSlash(strings.TrimSpace(f.Path))
		if rel == "" || strings.HasPrefix(rel, "/") {
			sourceRelOK = false
			break
		}
		clean := path.Clean(rel)
		if clean == ".." || strings.HasPrefix(clean, "../") {
			sourceRelOK = false
			break
		}
	}
	checks = append(checks, StrictCheck{
		Name:    "source_lock_non_empty",
		Passed:  sourceNonEmpty,
		Details: "source.lock must contain at least one file",
	})
	checks = append(checks, StrictCheck{
		Name:    "source_lock_relative_paths",
		Passed:  sourceRelOK,
		Details: "source.lock paths must be relative and stay within source root",
	})

	checks = append(checks, StrictCheck{
		Name:    "llm_key_runtime_injection_only",
		Passed:  llmNoInlineSecret(ir.Clawfile),
		Details: "clawfile habitat.env must not inline configured llm api key env variable",
	})

	return checks
}

func llmNoInlineSecret(cfg v1.Clawfile) bool {
	if cfg.Agent.LLM.Provider == "" {
		return true
	}
	keyEnv := strings.TrimSpace(cfg.Agent.LLM.APIKeyEnv)
	if keyEnv == "" {
		return true
	}
	if cfg.Agent.Habitat.Env == nil {
		return true
	}
	_, exists := cfg.Agent.Habitat.Env[keyEnv]
	return !exists
}

func failedChecks(checks []StrictCheck) []string {
	out := make([]string, 0)
	for _, c := range checks {
		if !c.Passed {
			out = append(out, c.Name)
		}
	}
	sort.Strings(out)
	return out
}

func buildProvenance(createdAt string, manifest capsule.Manifest, src locks.SourceLock) Provenance {
	bi, _ := debug.ReadBuildInfo()
	modulePath := "unknown"
	moduleVersion := "unknown"
	if bi != nil {
		if bi.Main.Path != "" {
			modulePath = bi.Main.Path
		}
		if bi.Main.Version != "" {
			moduleVersion = bi.Main.Version
		}
	}
	return Provenance{
		Version:        "metaclaw.provenance/v1",
		CreatedAt:      createdAt,
		ToolModule:     modulePath,
		ToolVersion:    moduleVersion,
		GoVersion:      runtime.Version(),
		HostOS:         runtime.GOOS,
		HostArch:       runtime.GOARCH,
		SourceClawfile: manifest.SourceClawfile,
		GitCommit:      src.GitCommit,
		GitTree:        src.GitTree,
		SourceFiles:    len(src.Files),
	}
}

func makeReleaseID(capsuleID string) string {
	h := sha256.New()
	_, _ = io.WriteString(h, capsuleID)
	_, _ = io.WriteString(h, time.Now().UTC().Format(time.RFC3339Nano))
	return hex.EncodeToString(h.Sum(nil))[:16]
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

func copyDir(src, dst string) error {
	srcAbs, err := filepath.Abs(src)
	if err != nil {
		return err
	}
	return filepath.WalkDir(srcAbs, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(srcAbs, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if rel == "." {
			return os.MkdirAll(target, 0o755)
		}
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if d.Type()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := os.Symlink(link, target); err != nil {
				return err
			}
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		info, err := in.Stat()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			_ = out.Close()
			return err
		}
		return out.Close()
	})
}

func loadOrCreatePrivateKey(path string) (ed25519.PrivateKey, ed25519.PublicKey, bool, error) {
	if b, err := os.ReadFile(path); err == nil {
		priv, err := parsePrivateKeyPEM(b)
		if err != nil {
			return nil, nil, false, err
		}
		pub, ok := priv.Public().(ed25519.PublicKey)
		if !ok {
			return nil, nil, false, fmt.Errorf("unexpected public key type")
		}
		return priv, pub, false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, nil, false, err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, nil, false, err
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, false, err
	}
	if err := writePrivateKeyPEM(path, priv); err != nil {
		return nil, nil, false, err
	}
	return priv, pub, true, nil
}

func writePrivateKeyPEM(path string, key ed25519.PrivateKey) error {
	pkcs8, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return err
	}
	block := &pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8}
	return os.WriteFile(path, pem.EncodeToMemory(block), 0o600)
}

func parsePrivateKeyPEM(pemBytes []byte) (ed25519.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("invalid private key PEM")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	priv, ok := key.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("private key is not ed25519")
	}
	return priv, nil
}

func writePublicKeyPEM(path string, key ed25519.PublicKey) error {
	der, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		return err
	}
	block := &pem.Block{Type: "PUBLIC KEY", Bytes: der}
	return os.WriteFile(path, pem.EncodeToMemory(block), 0o644)
}

func loadPublicKey(path string) (ed25519.PublicKey, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(b)
	if block == nil {
		return nil, fmt.Errorf("invalid public key PEM")
	}
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	pub, ok := key.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("public key is not ed25519")
	}
	return pub, nil
}

func deriveKeyID(key ed25519.PublicKey) string {
	sum := sha256.Sum256(key)
	return "ed25519:" + hex.EncodeToString(sum[:8])
}
