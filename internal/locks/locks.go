package locks

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gmmakoele-ship-it/rafikiclaw/internal/capability"
	v1 "github.com/gmmakoele-ship-it/rafikiclaw/internal/claw/schema/v1"
)

type BundleLocks struct {
	Deps   DepsLock   `json:"deps"`
	Image  ImageLock  `json:"image"`
	Source SourceLock `json:"source"`
}

type DepsLock struct {
	Version string      `json:"version"`
	Skills  []SkillLock `json:"skills"`
}

type SkillLock struct {
	Path    string `json:"path,omitempty"`
	ID      string `json:"id,omitempty"`
	Version string `json:"version,omitempty"`
	Digest  string `json:"digest"`
}

type ImageLock struct {
	Version string `json:"version"`
	Image   string `json:"image"`
	Digest  string `json:"digest"`
}

type SourceLock struct {
	Version   string     `json:"version"`
	GitCommit string     `json:"gitCommit,omitempty"`
	GitTree   string     `json:"gitTree,omitempty"`
	Files     []FileHash `json:"files"`
}

type FileHash struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

func Generate(cfg v1.Clawfile, clawfilePath string, outputDir string) (BundleLocks, error) {
	deps, err := buildDepsLock(cfg, filepath.Dir(clawfilePath))
	if err != nil {
		return BundleLocks{}, err
	}
	img := buildImageLock(cfg)
	srcRoot := filepath.Dir(clawfilePath)
	excludes := []string{".git", ".metaclaw"}
	if rel := relativeIfInside(srcRoot, outputDir); rel != "" {
		excludes = append(excludes, rel)
	}
	src, err := buildSourceLock(srcRoot, excludes)
	if err != nil {
		return BundleLocks{}, err
	}
	return BundleLocks{Deps: deps, Image: img, Source: src}, nil
}

func buildDepsLock(cfg v1.Clawfile, base string) (DepsLock, error) {
	out := DepsLock{Version: "rafikiclaw.depslock/v1"}
	for _, s := range cfg.Agent.Skills {
		sl := SkillLock{Path: s.Path, ID: s.ID, Version: s.Version}
		if s.Path != "" {
			p := s.Path
			if !filepath.IsAbs(p) {
				p = filepath.Join(base, p)
			}
			h, err := hashSkillPath(p)
			if err != nil {
				return DepsLock{}, fmt.Errorf("hash skill path %s: %w", s.Path, err)
			}
			sl.Digest = "sha256:" + h
		} else {
			target := s.ID + "@" + s.Version
			if s.Digest != "" {
				target += ":" + s.Digest
			}
			sum := sha256.Sum256([]byte(target))
			sl.Digest = "sha256:" + hex.EncodeToString(sum[:])
		}
		out.Skills = append(out.Skills, sl)
	}
	sort.Slice(out.Skills, func(i, j int) bool {
		return sortSkillKey(out.Skills[i]) < sortSkillKey(out.Skills[j])
	})
	return out, nil
}

func sortSkillKey(s SkillLock) string {
	if s.Path != "" {
		return "path:" + s.Path
	}
	return "id:" + s.ID + "@" + s.Version
}

func buildImageLock(cfg v1.Clawfile) ImageLock {
	image := cfg.Agent.Runtime.Image
	sum := sha256.Sum256([]byte(image))
	return ImageLock{
		Version: "rafikiclaw.imagelock/v1",
		Image:   image,
		Digest:  "sha256:" + hex.EncodeToString(sum[:]),
	}
}

func buildSourceLock(root string, excludes []string) (SourceLock, error) {
	out := SourceLock{Version: "rafikiclaw.sourcelock/v1"}
	commit, tree := gitMetadata(root)
	out.GitCommit = commit
	out.GitTree = tree

	files, err := fileManifest(root, excludes)
	if err != nil {
		return SourceLock{}, err
	}
	out.Files = files
	return out, nil
}

func gitMetadata(root string) (string, string) {
	cmdCommit := exec.Command("git", "-C", root, "rev-parse", "HEAD")
	cmdTree := exec.Command("git", "-C", root, "rev-parse", "HEAD^{tree}")
	bCommit, err1 := cmdCommit.Output()
	bTree, err2 := cmdTree.Output()
	if err1 != nil || err2 != nil {
		return "", ""
	}
	return strings.TrimSpace(string(bCommit)), strings.TrimSpace(string(bTree))
}

func fileManifest(root string, excludes []string) ([]FileHash, error) {
	var out []FileHash
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve source root: %w", err)
	}
	rootEval, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return nil, fmt.Errorf("resolve source root symlinks: %w", err)
	}
	excludeSet := make(map[string]struct{}, len(excludes))
	for _, e := range excludes {
		if e == "" || e == "." {
			continue
		}
		excludeSet[filepath.ToSlash(filepath.Clean(e))] = struct{}{}
	}
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			relSlash := filepath.ToSlash(rel)
			if _, ok := excludeSet[relSlash]; ok {
				return filepath.SkipDir
			}
			return nil
		}
		relSlash := filepath.ToSlash(rel)
		if shouldExcludeFile(relSlash, excludeSet) {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			h, err := hashSymlink(rootEval, path)
			if err != nil {
				return err
			}
			out = append(out, FileHash{Path: relSlash, SHA256: h})
			return nil
		}
		h, err := hashFile(path)
		if err != nil {
			return err
		}
		out = append(out, FileHash{Path: relSlash, SHA256: h})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func hashSymlink(root string, linkPath string) (string, error) {
	target, err := os.Readlink(linkPath)
	if err != nil {
		return "", fmt.Errorf("read symlink %s: %w", linkPath, err)
	}
	resolvedTarget, err := resolveSymlinkTarget(linkPath, target)
	if err != nil {
		return "", err
	}
	inside, err := isWithinRoot(root, resolvedTarget)
	if err != nil {
		return "", err
	}
	if !inside {
		return "", fmt.Errorf("symlink %s points outside source root", linkPath)
	}
	sum := sha256.Sum256([]byte("symlink:" + filepath.ToSlash(target)))
	return hex.EncodeToString(sum[:]), nil
}

func resolveSymlinkTarget(linkPath string, target string) (string, error) {
	resolved := target
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(filepath.Dir(linkPath), resolved)
	}
	absResolved, err := filepath.Abs(resolved)
	if err != nil {
		return "", fmt.Errorf("resolve symlink absolute path %s: %w", linkPath, err)
	}
	evalResolved, err := filepath.EvalSymlinks(absResolved)
	if err != nil {
		return "", fmt.Errorf("resolve symlink target %s: %w", linkPath, err)
	}
	return evalResolved, nil
}

func isWithinRoot(root string, target string) (bool, error) {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false, fmt.Errorf("compare path to source root: %w", err)
	}
	rel = filepath.Clean(rel)
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false, nil
	}
	return true, nil
}

func hashPath(path string) (string, error) {
	st, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !st.IsDir() {
		return hashFile(path)
	}
	entries, err := fileManifest(path, []string{".git", ".metaclaw"})
	if err != nil {
		return "", err
	}
	h := sha256.New()
	for _, e := range entries {
		_, _ = io.WriteString(h, e.Path)
		_, _ = io.WriteString(h, e.SHA256)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func hashSkillPath(path string) (string, error) {
	st, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if st.IsDir() {
		return hashPath(path)
	}
	fileHash, err := hashFile(path)
	if err != nil {
		return "", err
	}
	contractPath, ok, err := capability.DiscoverContractPath(path)
	if err != nil {
		return "", err
	}
	if !ok {
		return fileHash, nil
	}
	contractHash, err := hashFile(contractPath)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	_, _ = io.WriteString(h, filepath.Base(path))
	_, _ = io.WriteString(h, fileHash)
	_, _ = io.WriteString(h, filepath.Base(contractPath))
	_, _ = io.WriteString(h, contractHash)
	return hex.EncodeToString(h.Sum(nil)), nil
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func relativeIfInside(root string, target string) string {
	if target == "" {
		return ""
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return ""
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return ""
	}
	rel, err := filepath.Rel(absRoot, absTarget)
	if err != nil {
		return ""
	}
	rel = filepath.Clean(rel)
	if rel == "." || strings.HasPrefix(rel, "..") {
		return ""
	}
	return rel
}

func shouldExcludeFile(rel string, excludeSet map[string]struct{}) bool {
	for ex := range excludeSet {
		if rel == ex || strings.HasPrefix(rel, ex+"/") {
			return true
		}
	}
	return false
}
