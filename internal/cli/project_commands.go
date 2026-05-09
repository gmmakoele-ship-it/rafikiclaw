package cli

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gmmakoele-ship-it/rafikiclaw/internal/project"
)

func runProject(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: metaclaw project <init|upgrade> ...")
		return 1
	}
	switch args[0] {
	case "init":
		return runProjectInit(args[1:])
	case "upgrade":
		return runProjectUpgrade(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown project command: %s\n", args[0])
		fmt.Fprintln(os.Stderr, "usage: metaclaw project <init|upgrade> ...")
		return 1
	}
}

func runProjectInit(args []string) int {
	args = reorderFlags(args, map[string]bool{
		"--project-dir":   true,
		"--host-data-dir": true,
		"--template-dir":  true,
		"--template-repo": true,
		"--template-path": true,
		"--ref":           true,
		"--force":         false,
	})
	fs := flag.NewFlagSet("project init", flag.ContinueOnError)
	var projectDir string
	var hostDataDir string
	var templateDir string
	var templateRepo string
	var templatePath string
	var ref string
	var force bool
	fs.StringVar(&projectDir, "project-dir", "", "project directory")
	fs.StringVar(&hostDataDir, "host-data-dir", "", "host data directory (default <project>/.metaclaw)")
	fs.StringVar(&templateDir, "template-dir", "", "local template directory (alternative to --template-repo/--template-path)")
	fs.StringVar(&templateRepo, "template-repo", "", "git template repo URL (e.g. https://github.com/org/repo.git)")
	fs.StringVar(&templatePath, "template-path", "", "template subdirectory within repo")
	fs.StringVar(&ref, "ref", "main", "git ref (branch or tag)")
	fs.BoolVar(&force, "force", false, "allow using a non-empty project directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if len(fs.Args()) != 0 {
		fmt.Fprintln(os.Stderr, "usage: metaclaw project init --project-dir=... (--template-dir=... | --template-repo=... --template-path=...) [--ref=main] [--force]")
		return 1
	}
	if strings.TrimSpace(projectDir) == "" {
		fmt.Fprintln(os.Stderr, "project init failed: --project-dir is required")
		return 1
	}
	absProject, err := filepath.Abs(strings.TrimSpace(projectDir))
	if err != nil {
		fmt.Fprintf(os.Stderr, "project init failed: resolve project dir: %v\n", err)
		return 1
	}

	var src project.TemplateSource
	if strings.TrimSpace(templateDir) != "" {
		abs, err := filepath.Abs(strings.TrimSpace(templateDir))
		if err != nil {
			fmt.Fprintf(os.Stderr, "project init failed: resolve --template-dir: %v\n", err)
			return 1
		}
		src = project.TemplateSource{Kind: project.TemplateSourceKindLocal, Dir: abs}
	} else {
		if strings.TrimSpace(templateRepo) == "" || strings.TrimSpace(templatePath) == "" {
			fmt.Fprintln(os.Stderr, "project init failed: provide --template-dir or (--template-repo and --template-path)")
			return 1
		}
		src = project.TemplateSource{
			Kind: project.TemplateSourceKindGit,
			Repo: strings.TrimSpace(templateRepo),
			Ref:  strings.TrimSpace(ref),
			Path: strings.TrimSpace(templatePath),
		}
	}

	res, err := project.Init(project.InitOptions{
		ProjectDir:  absProject,
		HostDataDir: hostDataDir,
		Template:    src,
		Force:       force,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "project init failed: %v\n", err)
		return 1
	}
	fmt.Printf("project ready: %s\n", absProject)
	fmt.Printf("template: %s\n", res.TemplateID)
	if res.TemplateCommit != "" {
		fmt.Printf("template_commit: %s\n", res.TemplateCommit)
	}
	fmt.Printf("files: %d\n", res.CreatedFiles)
	return 0
}

func runProjectUpgrade(args []string) int {
	args = reorderFlags(args, map[string]bool{
		"--project-dir":   true,
		"--host-data-dir": true,
		"--template-dir":  true,
		"--template-repo": true,
		"--template-path": true,
		"--ref":           true,
		"--force":         false,
		"--dry-run":       false,
	})
	fs := flag.NewFlagSet("project upgrade", flag.ContinueOnError)
	var projectDir string
	var hostDataDir string
	var templateDir string
	var templateRepo string
	var templatePath string
	var ref string
	var force bool
	var dryRun bool
	fs.StringVar(&projectDir, "project-dir", ".", "project directory")
	fs.StringVar(&hostDataDir, "host-data-dir", "", "host data directory (default <project>/.metaclaw)")
	fs.StringVar(&templateDir, "template-dir", "", "override: local template directory")
	fs.StringVar(&templateRepo, "template-repo", "", "override: git template repo URL")
	fs.StringVar(&templatePath, "template-path", "", "override: template subdirectory within repo")
	fs.StringVar(&ref, "ref", "main", "override: git ref (branch or tag)")
	fs.BoolVar(&force, "force", false, "overwrite managed files even if locally modified (backs up to .metaclaw/upgrade-backups)")
	fs.BoolVar(&dryRun, "dry-run", false, "show what would change without writing files")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if len(fs.Args()) != 0 {
		fmt.Fprintln(os.Stderr, "usage: metaclaw project upgrade [--project-dir=.] [--force] [--dry-run]")
		return 1
	}

	absProject, err := filepath.Abs(strings.TrimSpace(projectDir))
	if err != nil {
		fmt.Fprintf(os.Stderr, "project upgrade failed: resolve project dir: %v\n", err)
		return 1
	}

	// Load lock, unless template overrides are provided.
	effectiveHostDataDir := hostDataDir
	if strings.TrimSpace(effectiveHostDataDir) == "" {
		effectiveHostDataDir = project.DefaultHostDataDir(absProject)
	}

	lock, lockErr := project.LoadLock(effectiveHostDataDir)
	var src project.TemplateSource
	if lockErr == nil {
		src = lock.Template
	}

	// Allow overrides if lock is missing or user wants to force a different template source.
	if strings.TrimSpace(templateDir) != "" {
		abs, err := filepath.Abs(strings.TrimSpace(templateDir))
		if err != nil {
			fmt.Fprintf(os.Stderr, "project upgrade failed: resolve --template-dir: %v\n", err)
			return 1
		}
		src = project.TemplateSource{Kind: project.TemplateSourceKindLocal, Dir: abs}
	}
	if strings.TrimSpace(templateRepo) != "" || strings.TrimSpace(templatePath) != "" {
		if strings.TrimSpace(templateRepo) == "" || strings.TrimSpace(templatePath) == "" {
			fmt.Fprintln(os.Stderr, "project upgrade failed: provide both --template-repo and --template-path")
			return 1
		}
		src = project.TemplateSource{
			Kind: project.TemplateSourceKindGit,
			Repo: strings.TrimSpace(templateRepo),
			Ref:  strings.TrimSpace(ref),
			Path: strings.TrimSpace(templatePath),
		}
	}
	if src.Kind == "" {
		if errors.Is(lockErr, os.ErrNotExist) {
			fmt.Fprintln(os.Stderr, "project upgrade failed: missing .metaclaw/project.lock.json; re-run onboard/quickstart or pass --template-dir/--template-repo")
			return 1
		}
		fmt.Fprintf(os.Stderr, "project upgrade failed: cannot load project lock: %v\n", lockErr)
		return 1
	}

	res, err := project.Upgrade(project.UpgradeOptions{
		ProjectDir:  absProject,
		HostDataDir: hostDataDir,
		Template:    src,
		Force:       force,
		DryRun:      dryRun,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "project upgrade failed: %v\n", err)
		// Still print the summary if available.
	}

	fmt.Printf("template: %s\n", res.TemplateID)
	if res.TemplateCommit != "" {
		fmt.Printf("template_commit: %s\n", res.TemplateCommit)
	}
	fmt.Printf("updated: %d\n", len(res.Updated))
	fmt.Printf("added: %d\n", len(res.Added))
	fmt.Printf("skipped: %d\n", len(res.Skipped))
	fmt.Printf("conflicts: %d\n", len(res.Conflicts))

	printList := func(label string, items []string) {
		if len(items) == 0 {
			return
		}
		fmt.Printf("%s:\n", label)
		for _, it := range items {
			fmt.Printf("  %s\n", it)
		}
	}
	printList("updated_files", res.Updated)
	printList("added_files", res.Added)
	printList("conflicts", res.Conflicts)

	if err != nil {
		return 1
	}
	return 0
}
