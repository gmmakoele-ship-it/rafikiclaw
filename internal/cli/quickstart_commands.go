package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	goruntime "runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gmmakoele-ship-it/rafikiclaw/internal/project"
)

type doctorCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

type doctorReport struct {
	SelectedRuntime string        `json:"selectedRuntime,omitempty"`
	RuntimeBin      string        `json:"runtimeBin,omitempty"`
	Checks          []doctorCheck `json:"checks"`
}

type doctorOptions struct {
	Runtime        string
	VaultPath      string
	LLMKeyEnv      string
	WebKeyEnv      string
	RequireLLMKey  bool
	CheckJQ        bool
	CheckPython    bool
	RequireVault   bool
	CheckNetwork   bool
	CheckDiskSpace bool
	DiskSpaceMinMB int
}

type quickstartOptions struct {
	ProjectDir  string
	VaultPath   string
	VaultWrite  bool
	Runtime     string
	LLMKeyEnv   string
	WebKeyEnv   string
	Profile     string
	TemplateDir string
	SkipBuild   bool
	NoRun       bool
	Force       bool
}

type obsidianProfile struct {
	Name           string
	NetworkMode    string
	RenderMode     string
	RetrievalScope string
	WriteConfirm   string
	SaveDefaultDir string
}

const (
	doctorStatusPass = "pass"
	doctorStatusWarn = "warn"
	doctorStatusFail = "fail"

	quickstartDefaultImageRepo = "rafikiclaw/obsidian-terminal-bot"
	quickstartDefaultImageTag  = "local"
)

var obsidianProfiles = map[string]obsidianProfile{
	"obsidian-chat": {
		Name:           "obsidian-chat",
		NetworkMode:    "none",
		RenderMode:     "glow",
		RetrievalScope: "limited",
		WriteConfirm:   "enter_once",
		SaveDefaultDir: "Research/Market-Reports",
	},
	"obsidian-research": {
		Name:           "obsidian-research",
		NetworkMode:    "outbound",
		RenderMode:     "glow",
		RetrievalScope: "all",
		WriteConfirm:   "diff_yes",
		SaveDefaultDir: "Research/Market-Reports",
	},
}

func runDoctor(args []string) int {
	args = reorderFlags(args, map[string]bool{
		"--runtime":          true,
		"--vault":            true,
		"--llm-key-env":      true,
		"--web-key-env":      true,
		"--require-llm-key":  false,
		"--check-network":    false,
		"--check-disk-space": false,
		"--disk-space-min":   true,
		"--json":             false,
	})

	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	opts := doctorOptions{
		Runtime:        "auto",
		LLMKeyEnv:      "OPENAI_FORMAT_API_KEY",
		WebKeyEnv:      "TAVILY_API_KEY",
		CheckJQ:        true,
		CheckPython:    true,
		CheckNetwork:   true,
		CheckDiskSpace: true,
		DiskSpaceMinMB:  512,
	}
	var asJSON bool
	fs.StringVar(&opts.Runtime, "runtime", opts.Runtime, "runtime target (auto|apple_container|podman|docker)")
	fs.StringVar(&opts.VaultPath, "vault", "", "vault path to validate")
	fs.StringVar(&opts.LLMKeyEnv, "llm-key-env", opts.LLMKeyEnv, "LLM API key env name")
	fs.StringVar(&opts.WebKeyEnv, "web-key-env", opts.WebKeyEnv, "web search API key env name")
	fs.BoolVar(&opts.RequireLLMKey, "require-llm-key", false, "treat missing llm key env as failure")
	fs.BoolVar(&opts.CheckNetwork, "check-network", true, "check network/connectivity")
	fs.BoolVar(&opts.CheckDiskSpace, "check-disk-space", true, "check available disk space")
	fs.IntVar(&opts.DiskSpaceMinMB, "disk-space-min", opts.DiskSpaceMinMB, "minimum required disk space in MB")
	fs.BoolVar(&asJSON, "json", false, "json output")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if len(fs.Args()) != 0 {
		fmt.Fprintln(os.Stderr, "usage: rafikiclaw doctor [--runtime=auto|apple_container|podman|docker] [--vault=/path] [--llm-key-env=OPENAI_FORMAT_API_KEY] [--web-key-env=TAVILY_API_KEY] [--require-llm-key] [--check-network] [--check-disk-space] [--disk-space-min=MB] [--json]")
		return 1
	}

	report, err := collectDoctorReport(opts)
	if asJSON {
		b, _ := json.MarshalIndent(report, "", "  ")
		fmt.Println(string(b))
	} else {
		printDoctorReport(report)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "doctor failed: %v\n", err)
		return 1
	}
	return 0
}

func runQuickstart(args []string) int {
	args = reorderFlags(args, map[string]bool{
		"--project-dir":  true,
		"--vault":        true,
		"--vault-write":  false,
		"--runtime":      true,
		"--llm-key-env":  true,
		"--web-key-env":  true,
		"--profile":      true,
		"--template-dir": true,
		"--skip-build":   false,
		"--no-run":       false,
		"--force":        false,
	})

	fs := flag.NewFlagSet("quickstart", flag.ContinueOnError)
	opts := quickstartOptions{
		ProjectDir: "./metaclaw-obsidian-bot",
		Runtime:    "auto",
		LLMKeyEnv:  "OPENAI_FORMAT_API_KEY",
		WebKeyEnv:  "TAVILY_API_KEY",
		Profile:    "obsidian-chat",
	}
	fs.StringVar(&opts.ProjectDir, "project-dir", opts.ProjectDir, "project directory")
	fs.StringVar(&opts.VaultPath, "vault", "", "absolute vault path (interactive prompt if omitted)")
	fs.BoolVar(&opts.VaultWrite, "vault-write", false, "mount vault read-write inside container (less safe; default is read-only)")
	fs.StringVar(&opts.Runtime, "runtime", opts.Runtime, "runtime target (auto|apple_container|podman|docker)")
	fs.StringVar(&opts.LLMKeyEnv, "llm-key-env", opts.LLMKeyEnv, "LLM API key env name")
	fs.StringVar(&opts.WebKeyEnv, "web-key-env", opts.WebKeyEnv, "web search API key env name")
	fs.StringVar(&opts.Profile, "profile", opts.Profile, "quickstart profile (obsidian-chat|obsidian-research)")
	fs.StringVar(&opts.TemplateDir, "template-dir", "", "optional local path to obsidian bot template directory")
	fs.BoolVar(&opts.SkipBuild, "skip-build", false, "skip image build")
	fs.BoolVar(&opts.NoRun, "no-run", false, "prepare project only, do not launch chat")
	fs.BoolVar(&opts.Force, "force", false, "allow using a non-empty project directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	remaining := fs.Args()
	if len(remaining) != 1 || remaining[0] != "obsidian" {
		fmt.Fprintln(os.Stderr, "usage: metaclaw quickstart obsidian [--project-dir=./my-bot] [--vault=/abs/path/to/vault] [--vault-write] [--runtime=auto|apple_container|podman|docker] [--profile=obsidian-chat] [--skip-build] [--no-run]")
		return 1
	}

	profile, ok := resolveObsidianProfile(opts.Profile)
	if !ok {
		fmt.Fprintf(os.Stderr, "quickstart failed: unsupported profile %q\n", opts.Profile)
		return 1
	}

	if !wizardEnvNameRef.MatchString(strings.TrimSpace(opts.LLMKeyEnv)) {
		fmt.Fprintf(os.Stderr, "quickstart failed: --llm-key-env must be a valid environment variable name\n")
		return 1
	}
	if !wizardEnvNameRef.MatchString(strings.TrimSpace(opts.WebKeyEnv)) {
		fmt.Fprintf(os.Stderr, "quickstart failed: --web-key-env must be a valid environment variable name\n")
		return 1
	}

	var err error
	opts.ProjectDir, err = filepath.Abs(strings.TrimSpace(opts.ProjectDir))
	if err != nil {
		fmt.Fprintf(os.Stderr, "quickstart failed: resolve project dir: %v\n", err)
		return 1
	}
	if strings.TrimSpace(opts.VaultPath) == "" {
		opts.VaultPath, err = promptQuickstartVaultPath()
		if err != nil {
			fmt.Fprintf(os.Stderr, "quickstart failed: %v\n", err)
			return 1
		}
	}
	opts.VaultPath, err = filepath.Abs(strings.TrimSpace(opts.VaultPath))
	if err != nil {
		fmt.Fprintf(os.Stderr, "quickstart failed: resolve vault path: %v\n", err)
		return 1
	}

	hostDataDir := filepath.Join(opts.ProjectDir, ".metaclaw")
	stateDir := filepath.Join(hostDataDir, "state")

	report, err := collectDoctorReport(doctorOptions{
		Runtime:       opts.Runtime,
		VaultPath:     opts.VaultPath,
		LLMKeyEnv:     opts.LLMKeyEnv,
		WebKeyEnv:     opts.WebKeyEnv,
		RequireLLMKey: false,
		CheckJQ:       !opts.SkipBuild,
		CheckPython:   !opts.NoRun,
		RequireVault:  true,
	})
	printDoctorReport(report)
	if err != nil {
		fmt.Fprintf(os.Stderr, "quickstart failed: %v\n", err)
		return 1
	}

	templateDir, err := resolveObsidianTemplateDir(opts.TemplateDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "quickstart failed: %v\n", err)
		return 1
	}

	if err := scaffoldObsidianProject(templateDir, opts.ProjectDir, opts.VaultPath, opts.VaultWrite, hostDataDir, opts.LLMKeyEnv, opts.WebKeyEnv, report.SelectedRuntime, profile, opts.Force); err != nil {
		fmt.Fprintf(os.Stderr, "quickstart failed: %v\n", err)
		return 1
	}

	// Write a generic project lock so future upgrades can refresh managed template files in-place
	// without overwriting user-owned data like agent.claw, vault content, or .env.
	{
		src := project.TemplateSource{
			Kind: project.TemplateSourceKindGit,
			Repo: "https://github.com/gmmakoele-ship-it/rafikiclaw-examples.git",
			Ref:  "main",
			Path: "examples/obsidian-terminal-bot-advanced",
		}
		// If user explicitly provided a template dir, treat it as a local template source.
		if strings.TrimSpace(opts.TemplateDir) != "" {
			src = project.TemplateSource{Kind: project.TemplateSourceKindLocal, Dir: templateDir}
		}
		manifest, err := project.LoadManifest(templateDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: cannot load template manifest (%s): %v\n", project.ManifestFilename, err)
		} else {
			managed, err := project.ManagedFiles(templateDir, manifest)
			if err != nil {
				fmt.Fprintf(os.Stderr, "warning: cannot compute managed files: %v\n", err)
			} else if hashes, err := project.HashManagedFiles(opts.ProjectDir, managed); err != nil {
				fmt.Fprintf(os.Stderr, "warning: cannot hash managed files: %v\n", err)
			} else {
				lock := project.ProjectLock{
					SchemaVersion:  1,
					Template:       src,
					TemplateID:     manifest.ID,
					TemplateCommit: gitCommitForDir(templateDir),
					InstalledAtUTC: time.Now().UTC().Format(time.RFC3339),
					ManagedFiles:   hashes,
				}
				if err := project.WriteLock(hostDataDir, lock); err != nil {
					fmt.Fprintf(os.Stderr, "warning: cannot write project lock: %v\n", err)
				}
			}
		}
	}

	fmt.Printf("quickstart ready: %s\n", opts.ProjectDir)
	fmt.Printf("vault: %s\n", opts.VaultPath)
	if opts.VaultWrite {
		fmt.Printf("vault access: read-write (less safe)\n")
	} else {
		fmt.Printf("vault access: read-only (recommended)\n")
	}
	fmt.Printf("host data: %s\n", hostDataDir)
	fmt.Printf("profile: %s\n", profile.Name)
	fmt.Printf("runtime: %s\n", report.SelectedRuntime)

	effectiveRuntime := report.SelectedRuntime
	if !opts.SkipBuild {
		candidates := buildQuickstartRuntimeCandidates(opts.Runtime, report.SelectedRuntime)
		var lastErr error
		var built bool
		for i, target := range candidates {
			bin := runtimeBinaryForTarget(target)
			if bin == "" || !commandExists(bin) {
				continue
			}
			if i == 0 {
				fmt.Printf("building bot image with %s...\n", target)
			} else {
				fmt.Printf("retrying bot image build with fallback runtime %s...\n", target)
			}
			if err := buildQuickstartImage(opts.ProjectDir, target, bin); err != nil {
				lastErr = err
				if strings.TrimSpace(opts.Runtime) != "auto" {
					fmt.Fprintf(os.Stderr, "quickstart failed: build image with %s: %v\n", target, err)
					return 1
				}
				fmt.Fprintf(os.Stderr, "warning: build failed on %s: %v\n", target, err)
				continue
			}
			effectiveRuntime = target
			built = true
			if target != report.SelectedRuntime {
				if err := rewriteQuickstartRuntimeDefault(filepath.Join(opts.ProjectDir, "chat.sh"), target); err != nil {
					fmt.Fprintf(os.Stderr, "quickstart failed: update chat runtime default: %v\n", err)
					return 1
				}
				fmt.Printf("runtime fallback selected: %s\n", target)
			}
			break
		}
		if !built {
			if lastErr == nil {
				lastErr = errors.New("no available runtime could build the image")
			}
			fmt.Fprintf(os.Stderr, "quickstart failed: build image: %v\n", lastErr)
			return 1
		}
	}

	if opts.NoRun {
		fmt.Println("project prepared. launch chat when ready:")
		fmt.Printf("  cd %s\n", opts.ProjectDir)
		fmt.Printf("  export %s=...\n", opts.LLMKeyEnv)
		fmt.Printf("  ./chat.sh\n")
		return 0
	}

	exePath := "rafikiclaw"
	if exe, err := os.Executable(); err == nil {
		exePath = exe
	}
	fmt.Println("launching chat...")
	if err := runScript(filepath.Join(opts.ProjectDir, "chat.sh"), opts.ProjectDir, map[string]string{
		"METACLAW_BIN":      exePath,
		"RUNTIME_TARGET":    effectiveRuntime,
		"LLM_KEY_ENV":       opts.LLMKeyEnv,
		"TAVILY_KEY_ENV":    opts.WebKeyEnv,
		"BOT_HOST_DATA_DIR": hostDataDir,
		"BOT_STATE_DIR":     stateDir,
	}, true); err != nil {
		fmt.Fprintf(os.Stderr, "quickstart failed: chat.sh: %v\n", err)
		return 1
	}
	return 0
}

func collectDoctorReport(opts doctorOptions) (doctorReport, error) {
	report := doctorReport{Checks: make([]doctorCheck, 0, 8)}
	add := func(name, status, detail string) {
		report.Checks = append(report.Checks, doctorCheck{Name: name, Status: status, Detail: detail})
	}

	runtimeTarget, runtimeBin, runtimeHealth, err := resolveRequestedRuntime(opts.Runtime)
	if err != nil {
		add("runtime", doctorStatusFail, err.Error())
	} else {
		report.SelectedRuntime = runtimeTarget
		report.RuntimeBin = runtimeBin
		add("runtime", doctorStatusPass, fmt.Sprintf("%s (%s)", runtimeTarget, runtimeBin))
		add("runtime_health", doctorStatusPass, runtimeHealth)
	}

	if strings.TrimSpace(opts.VaultPath) != "" {
		if st, err := os.Stat(opts.VaultPath); err != nil {
			status := doctorStatusWarn
			if opts.RequireVault {
				status = doctorStatusFail
			}
			add("vault", status, fmt.Sprintf("not accessible: %v", err))
		} else if !st.IsDir() {
			status := doctorStatusWarn
			if opts.RequireVault {
				status = doctorStatusFail
			}
			add("vault", status, "path exists but is not a directory")
		} else {
			add("vault", doctorStatusPass, opts.VaultPath)
		}
	}

	llmEnv := strings.TrimSpace(opts.LLMKeyEnv)
	if llmEnv == "" {
		llmEnv = "OPENAI_FORMAT_API_KEY"
	}
	if strings.TrimSpace(os.Getenv(llmEnv)) == "" {
		status := doctorStatusWarn
		if opts.RequireLLMKey {
			status = doctorStatusFail
		}
		add("llm_key", status, fmt.Sprintf("%s not set", llmEnv))
	} else {
		add("llm_key", doctorStatusPass, fmt.Sprintf("%s is set", llmEnv))
	}

	webEnv := strings.TrimSpace(opts.WebKeyEnv)
	if webEnv == "" {
		webEnv = "TAVILY_API_KEY"
	}
	if strings.TrimSpace(os.Getenv(webEnv)) == "" {
		add("web_key", doctorStatusWarn, fmt.Sprintf("%s not set (optional)", webEnv))
	} else {
		add("web_key", doctorStatusPass, fmt.Sprintf("%s is set", webEnv))
	}

	if opts.CheckJQ {
		needsJQ := runtimeTarget == "apple_container"
		if commandExists("jq") {
			add("jq", doctorStatusPass, "available")
		} else if needsJQ {
			add("jq", doctorStatusFail, "jq not found (required for apple_container image digest resolution)")
		} else {
			add("jq", doctorStatusWarn, "jq not found (optional for docker/podman builds)")
		}
	}
	if opts.CheckPython {
		if commandExists("python3") {
			add("python3", doctorStatusPass, "available")
		} else {
			add("python3", doctorStatusFail, "python3 not found (required by chat.sh)")
		}
	}


	if opts.CheckDiskSpace {
		checkDiskSpace(opts.DiskSpaceMinMB, add)
	}

	if opts.CheckNetwork {
		checkNetworkConnectivity(add)
	}

	failed := make([]string, 0, 4)
	for _, c := range report.Checks {
		if c.Status == doctorStatusFail {
			failed = append(failed, c.Name)
		}
	}
	if len(failed) > 0 {
		sort.Strings(failed)
		return report, fmt.Errorf("failing checks: %s", strings.Join(failed, ", "))
	}
	return report, nil
}

func printDoctorReport(report doctorReport) {
	fmt.Println("doctor:")
	for _, c := range report.Checks {
		prefix := "OK"
		switch c.Status {
		case doctorStatusWarn:
			prefix = "WARN"
		case doctorStatusFail:
			prefix = "FAIL"
		}
		fmt.Printf("  [%s] %s: %s\n", prefix, c.Name, c.Detail)
	}
	if report.SelectedRuntime != "" {
		fmt.Printf("selected runtime: %s\n", report.SelectedRuntime)
	}
}

func resolveRequestedRuntime(requested string) (string, string, string, error) {
	rt := strings.TrimSpace(requested)
	if rt == "" {
		rt = "auto"
	}
	if rt == "auto" {
		var errs []string
		found := false
		for _, candidate := range runtimeProbeOrder() {
			bin := runtimeBinaryForTarget(candidate)
			if bin == "" || !commandExists(bin) {
				continue
			}
			found = true
			detail, err := checkRuntimeHealth(candidate, bin)
			if err == nil {
				return candidate, bin, detail, nil
			}
			errs = append(errs, fmt.Sprintf("%s: %v", candidate, err))
		}
		if found {
			return "", "", "", fmt.Errorf("no healthy runtime found (tried %s)", strings.Join(errs, "; "))
		}
		return "", "", "", errors.New("no supported runtime found (install apple container, podman, or docker)")
	}
	bin := runtimeBinaryForTarget(rt)
	if bin == "" {
		return "", "", "", fmt.Errorf("invalid runtime %q", rt)
	}
	if !commandExists(bin) {
		return "", "", "", fmt.Errorf("runtime %s is not available (missing binary: %s)", rt, bin)
	}
	detail, err := checkRuntimeHealth(rt, bin)
	if err != nil {
		return "", "", "", fmt.Errorf("runtime %s is installed but not usable: %v", rt, err)
	}
	return rt, bin, detail, nil
}

func runtimeProbeOrder() []string {
	if goruntime.GOOS == "darwin" {
		return []string{"apple_container", "podman", "docker"}
	}
	return []string{"podman", "docker", "apple_container"}
}

func buildQuickstartRuntimeCandidates(requested, selected string) []string {
	rt := strings.TrimSpace(requested)
	seen := map[string]struct{}{}
	out := make([]string, 0, 4)
	add := func(target string) {
		target = strings.TrimSpace(target)
		if target == "" {
			return
		}
		if _, ok := seen[target]; ok {
			return
		}
		seen[target] = struct{}{}
		out = append(out, target)
	}

	if rt == "" || rt == "auto" {
		add(selected)
		for _, candidate := range runtimeProbeOrder() {
			add(candidate)
		}
		return out
	}
	add(selected)
	if len(out) == 0 {
		add(rt)
	}
	return out
}

func runtimeBinaryForTarget(target string) string {
	switch target {
	case "apple_container":
		return "container"
	case "podman":
		return "podman"
	case "docker":
		return "docker"
	default:
		return ""
	}
}

func resolveObsidianProfile(name string) (obsidianProfile, bool) {
	n := strings.TrimSpace(strings.ToLower(name))
	if n == "" {
		n = "obsidian-chat"
	}
	p, ok := obsidianProfiles[n]
	return p, ok
}

func checkRuntimeHealth(target, bin string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
	defer cancel()

	firstLine := func(s string) string {
		s = strings.TrimSpace(s)
		if s == "" {
			return ""
		}
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			return strings.TrimSpace(s[:i])
		}
		return s
	}

	switch target {
	case "docker":
		// `docker info` sometimes exits 0 while still printing a connectivity error on stderr,
		// so we rely on the server version field from `docker version` instead.
		stdout, stderr, err := runDoctorCmd(ctx, bin, "version", "--format", "{{.Server.Version}}")
		version := firstLine(stdout)
		if err != nil || version == "" || strings.Contains(strings.ToLower(stderr), "cannot connect to the docker daemon") {
			msg := firstLine(stderr)
			if msg == "" {
				msg = firstLine(stdout)
			}
			if msg == "" && err != nil {
				msg = err.Error()
			}
			if msg == "" {
				msg = "unknown error"
			}
			return "", fmt.Errorf("docker daemon not reachable (%s)", msg)
		}
		return fmt.Sprintf("docker daemon reachable (server %s)", version), nil
	case "podman":
		// Prefer a small formatted output, but fall back to plain `podman info` for older installs.
		stdout, stderr, err := runDoctorCmd(ctx, bin, "info", "--format", "{{.Version.Version}}")
		if err != nil {
			stdout, stderr, err = runDoctorCmd(ctx, bin, "info")
		}
		if err != nil {
			msg := firstLine(stderr)
			if msg == "" {
				msg = firstLine(stdout)
			}
			if msg == "" {
				msg = err.Error()
			}
			help := msg
			low := strings.ToLower(help)
			if strings.Contains(low, "machine") && strings.Contains(low, "start") {
				help = msg + " (try: podman machine start)"
			}
			return "", fmt.Errorf("podman not reachable (%s)", help)
		}
		return "podman reachable", nil
	case "apple_container":
		// Apple Container should at least report a version; some environments require permissions on first run.
		stdout, stderr, err := runDoctorCmd(ctx, bin, "--version")
		if err != nil {
			stdout, stderr, err = runDoctorCmd(ctx, bin, "version")
		}
		if err != nil {
			msg := firstLine(stderr)
			if msg == "" {
				msg = firstLine(stdout)
			}
			if msg == "" {
				msg = err.Error()
			}
			return "", fmt.Errorf("apple container not reachable (%s)", msg)
		}
		v := firstLine(stdout)
		if v == "" {
			return "apple container reachable", nil
		}
		return fmt.Sprintf("apple container reachable (%s)", v), nil
	default:
		return "ok", nil
	}
}

func runDoctorCmd(ctx context.Context, bin string, args ...string) (string, string, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	var out bytes.Buffer
	var errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	err := cmd.Run()
	return out.String(), errBuf.String(), err
}

func promptQuickstartVaultPath() (string, error) {
	if !isInteractiveTerminal() {
		return "", errors.New("--vault is required in non-interactive mode")
	}
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("Obsidian vault path: ")
		line, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
		value := strings.TrimSpace(line)
		if value != "" {
			return value, nil
		}
		if errors.Is(err, io.EOF) {
			return "", errors.New("input closed before vault path was provided")
		}
		fmt.Println("vault path is required")
	}
}

func isInteractiveTerminal() bool {
	st, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (st.Mode() & os.ModeCharDevice) != 0
}

func resolveObsidianTemplateDir(explicit string) (string, error) {
	explicit = strings.TrimSpace(explicit)
	if explicit != "" {
		abs, err := filepath.Abs(explicit)
		if err != nil {
			return "", fmt.Errorf("resolve --template-dir: %w", err)
		}
		if ok, err := hasObsidianTemplate(abs); err != nil {
			return "", err
		} else if !ok {
			return "", fmt.Errorf("template dir does not look like obsidian-terminal-bot-advanced: %s", abs)
		}
		return abs, nil
	}

	candidates := make([]string, 0, 8)
	if envDir := strings.TrimSpace(os.Getenv("METACLAW_EXAMPLES_DIR")); envDir != "" {
		candidates = append(candidates,
			filepath.Join(envDir, "examples", "obsidian-terminal-bot-advanced"),
			envDir,
		)
	}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(wd, "metaclaw-examples", "examples", "obsidian-terminal-bot-advanced"),
			filepath.Join(wd, "..", "metaclaw-examples", "examples", "obsidian-terminal-bot-advanced"),
		)
	}
	if exe, err := os.Executable(); err == nil {
		d := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(d, "..", "metaclaw-examples", "examples", "obsidian-terminal-bot-advanced"),
			filepath.Join(d, "..", "..", "metaclaw-examples", "examples", "obsidian-terminal-bot-advanced"),
		)
	}

	for _, c := range candidates {
		abs, err := filepath.Abs(c)
		if err != nil {
			continue
		}
		ok, err := hasObsidianTemplate(abs)
		if err == nil && ok {
			return abs, nil
		}
	}

	if commandExists("git") {
		cached, err := ensureCachedExamplesTemplate()
		if err == nil {
			return cached, nil
		}
	}

	return "", errors.New("cannot find obsidian template; install git for auto-fetch or pass --template-dir")
}

func hasObsidianTemplate(dir string) (bool, error) {
	required := []string{
		"agent.claw",
		"chat.sh",
		"chat_tui.py",
		"build_image.sh",
		"image/Dockerfile",
		"bot/chat_once.py",
		"agents/AGENTS.md",
		"agents/soul.md",
	}
	for _, rel := range required {
		path := filepath.Join(dir, rel)
		if _, err := os.Stat(path); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return false, nil
			}
			return false, err
		}
	}
	return true, nil
}

func ensureCachedExamplesTemplate() (string, error) {
	cacheRoot := filepath.Join(os.TempDir(), "metaclaw-quickstart-cache")
	repoDir := filepath.Join(cacheRoot, "metaclaw-examples")
	templateDir := filepath.Join(repoDir, "examples", "obsidian-terminal-bot-advanced")

	if err := os.MkdirAll(cacheRoot, 0o755); err != nil {
		return "", fmt.Errorf("create cache dir: %w", err)
	}

	if _, err := os.Stat(repoDir); err == nil {
		// The examples repo may be force-updated while still evolving. Prefer a hard sync to origin/main
		// so quickstart always uses the latest template when network is available.
		if err := syncGitRepoToMain(repoDir); err == nil {
			if ok, err := hasObsidianTemplate(templateDir); err == nil && ok {
				return templateDir, nil
			}
		}
		if ok, err := hasObsidianTemplate(templateDir); err == nil && ok {
			return templateDir, nil
		}
	}

	if err := runGit(cacheRoot, "clone", "--depth", "1", "https://github.com/gmmakoele-ship-it/rafikiclaw-examples.git", repoDir); err != nil {
		return "", err
	}
	if ok, err := hasObsidianTemplate(templateDir); err != nil {
		return "", err
	} else if !ok {
		return "", errors.New("cached metaclaw-examples missing obsidian template")
	}
	return templateDir, nil
}

func runGit(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run()
}

func syncGitRepoToMain(repoDir string) error {
	// Keep these best-effort and quiet: if network is unavailable, quickstart should still work
	// with whatever template is already cached.
	if err := runGit(repoDir, "fetch", "--prune", "--depth", "1", "origin", "main"); err != nil {
		return err
	}
	if err := runGit(repoDir, "reset", "--hard", "origin/main"); err != nil {
		return err
	}
	_ = runGit(repoDir, "clean", "-fdx")
	return nil
}

func gitCommitForDir(dir string) string {
	if !commandExists("git") {
		return ""
	}
	// Find the nearest parent containing .git.
	repo := ""
	cur := dir
	for i := 0; i < 16; i++ {
		if cur == "" || cur == string(filepath.Separator) {
			break
		}
		if st, err := os.Stat(filepath.Join(cur, ".git")); err == nil && st.IsDir() {
			repo = cur
			break
		}
		next := filepath.Dir(cur)
		if next == cur {
			break
		}
		cur = next
	}
	if repo == "" {
		return ""
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	cmd.Dir = repo
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return ""
	}
	return strings.TrimSpace(out.String())
}

func scaffoldObsidianProject(templateDir, projectDir, vaultPath string, vaultWrite bool, hostDataDir, llmKeyEnv, webKeyEnv, runtimeTarget string, profile obsidianProfile, force bool) error {
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		return fmt.Errorf("create project dir: %w", err)
	}
	if !force {
		entries, err := os.ReadDir(projectDir)
		if err != nil {
			return fmt.Errorf("read project dir: %w", err)
		}
		allowedTop := map[string]struct{}{
			".DS_Store": {},
		}

		// UX: allow a vault directory to exist inside the project directory.
		// This supports the onboarding default (<project>/vault) without requiring --force.
		vaultAbs, vErr := filepath.Abs(strings.TrimSpace(vaultPath))
		projAbs, pErr := filepath.Abs(strings.TrimSpace(projectDir))
		if vErr == nil && pErr == nil && isSubpath(vaultAbs, projAbs) && vaultAbs != projAbs {
			if rel, err := filepath.Rel(projAbs, vaultAbs); err == nil {
				rel = filepath.Clean(rel)
				first := strings.Split(rel, string(os.PathSeparator))[0]
				if first != "" && first != "." && first != ".." {
					allowedTop[first] = struct{}{}
				}
			}
		}

		var unexpected []string
		for _, e := range entries {
			name := e.Name()
			if _, ok := allowedTop[name]; ok {
				continue
			}
			unexpected = append(unexpected, name)
		}
		if len(unexpected) > 0 {
			sort.Strings(unexpected)
			return fmt.Errorf("project dir is not empty: %s (unexpected: %s; use --force to continue)", projectDir, strings.Join(unexpected, ", "))
		}
	}

	for _, rel := range []string{"agent.claw", "build_image.sh", "chat.sh", "chat_tui.py", "README.md", "bot", "image", "agents"} {
		src := filepath.Join(templateDir, rel)
		dst := filepath.Join(projectDir, rel)
		if err := copyTemplateEntry(src, dst); err != nil {
			return fmt.Errorf("copy %s: %w", rel, err)
		}
	}

	for _, dir := range []string{"config", "logs", "runtime", "workspace", "state"} {
		if err := os.MkdirAll(filepath.Join(hostDataDir, dir), 0o755); err != nil {
			return fmt.Errorf("create host data dir %s: %w", dir, err)
		}
	}

	if err := rewriteObsidianAgentFile(filepath.Join(projectDir, "agent.claw"), vaultPath, hostDataDir, profile.NetworkMode, vaultWrite); err != nil {
		return err
	}
	if err := rewriteQuickstartChatScript(filepath.Join(projectDir, "chat.sh"), hostDataDir, llmKeyEnv, webKeyEnv, runtimeTarget, profile); err != nil {
		return err
	}
	if err := writeObsidianProfileDefaults(filepath.Join(hostDataDir, "config", "ui.defaults.json"), profile); err != nil {
		return err
	}
	if err := writeQuickstartEnvExample(filepath.Join(projectDir, ".env.example"), llmKeyEnv, webKeyEnv); err != nil {
		return err
	}
	if err := writeQuickstartGitignore(filepath.Join(projectDir, ".gitignore")); err != nil {
		return err
	}
	return nil
}

func copyTemplateEntry(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		if strings.HasSuffix(filepath.Base(src), ".pyc") {
			return nil
		}
		return copyTemplateFile(src, dst, info.Mode())
	}
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			if d.Name() == "__pycache__" {
				return filepath.SkipDir
			}
			return os.MkdirAll(target, 0o755)
		}
		if strings.HasSuffix(d.Name(), ".pyc") {
			return nil
		}
		st, err := d.Info()
		if err != nil {
			return err
		}
		return copyTemplateFile(path, target, st.Mode())
	})
}

func copyTemplateFile(src, dst string, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode.Perm())
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return nil
}

func rewriteObsidianAgentFile(path, vaultPath, hostDataDir, networkMode string, vaultWrite bool) error {
	if networkMode != "none" && networkMode != "outbound" && networkMode != "all" {
		return fmt.Errorf("invalid profile network mode: %s", networkMode)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read agent.claw: %w", err)
	}
	text := string(b)
	text = strings.ReplaceAll(text, "/ABS/PATH/TO/OBSIDIAN_VAULT", vaultPath)
	text = strings.ReplaceAll(text, "/ABS/PATH/TO/BOT_HOST_DATA", hostDataDir)
	text = replaceFirstNetworkMode(text, networkMode)
	text = setMountReadOnlyByTarget(text, "/vault", !vaultWrite)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		return fmt.Errorf("write agent.claw: %w", err)
	}
	return nil
}

func replaceFirstNetworkMode(content, networkMode string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "mode:") {
			indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			lines[i] = indent + "mode: " + networkMode
			break
		}
	}
	return strings.Join(lines, "\n")
}

func stripOuterQuotesScalar(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 {
		if (value[0] == '\'' && value[len(value)-1] == '\'') || (value[0] == '"' && value[len(value)-1] == '"') {
			return strings.TrimSpace(value[1 : len(value)-1])
		}
	}
	return value
}

func setMountReadOnlyByTarget(content, targetPath string, readOnly bool) string {
	lines := strings.Split(content, "\n")

	inMounts := false
	mountsIndent := 0
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		indent := len(line) - len(strings.TrimLeft(line, " \t"))

		if trimmed == "mounts:" {
			inMounts = true
			mountsIndent = indent
			continue
		}
		if inMounts && indent <= mountsIndent && trimmed != "" && !strings.HasPrefix(trimmed, "-") {
			// Left mounts section.
			inMounts = false
		}
		if !inMounts {
			continue
		}

		if !strings.HasPrefix(trimmed, "target:") {
			continue
		}
		val := strings.TrimSpace(strings.TrimPrefix(trimmed, "target:"))
		val = stripOuterQuotesScalar(val)
		if val != targetPath {
			continue
		}

		roIndent := line[:indent]
		desired := roIndent + "readOnly: " + strconv.FormatBool(readOnly)

		// Look for an existing readOnly line in the same mount item.
		found := false
		insertAt := i + 1
		for j := i + 1; j < len(lines); j++ {
			jLine := lines[j]
			jTrim := strings.TrimSpace(jLine)
			jIndent := len(jLine) - len(strings.TrimLeft(jLine, " \t"))

			// Next mount item or leaving mounts.
			if jIndent <= mountsIndent+2 && strings.HasPrefix(jTrim, "-") {
				break
			}
			if jIndent <= mountsIndent && jTrim != "" && !strings.HasPrefix(jTrim, "-") {
				break
			}

			if strings.HasPrefix(jTrim, "readOnly:") {
				lines[j] = desired
				found = true
				break
			}
		}
		if !found {
			lines = append(lines[:insertAt], append([]string{desired}, lines[insertAt:]...)...)
		}

		// Only update the first matching mount target.
		return strings.Join(lines, "\n")
	}
	return content
}

func rewriteQuickstartChatScript(path, hostDataDir, llmKeyEnv, webKeyEnv, runtimeTarget string, profile obsidianProfile) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read chat.sh: %w", err)
	}
	text := string(b)
	text = strings.Replace(text, "${BOT_RENDER_MODE:-glow}", "${BOT_RENDER_MODE:-"+profile.RenderMode+"}", 1)
	text = strings.Replace(text, "${BOT_NETWORK_MODE:-none}", "${BOT_NETWORK_MODE:-"+profile.NetworkMode+"}", 1)

	// Optional local env file for convenience. It is gitignored by quickstart and should never be committed.
	if !strings.Contains(text, "$PROJECT_DIR/.env") {
		marker := "PROJECT_DIR=\"$(cd \"$(dirname \"$0\")\" && pwd)\""
		block := strings.Join([]string{
			`# Load project-local secrets (never commit .env)`,
			`if [ -f "$PROJECT_DIR/.env" ]; then`,
			`  set -a`,
			`  . "$PROJECT_DIR/.env"`,
			`  set +a`,
			`fi`,
		}, "\n")
		if strings.Contains(text, marker) {
			text = strings.Replace(text, marker, marker+"\n"+block, 1)
		} else {
			text = block + "\n" + text
		}
	}

	injection := strings.Join([]string{
		"export BOT_HOST_DATA_DIR=\"${BOT_HOST_DATA_DIR:-$PROJECT_DIR/.metaclaw}\"",
		"export BOT_STATE_DIR=\"${BOT_STATE_DIR:-$BOT_HOST_DATA_DIR/state}\"",
		"export RUNTIME_TARGET=\"${RUNTIME_TARGET:-" + runtimeTarget + "}\"",
		"export LLM_KEY_ENV=\"${LLM_KEY_ENV:-" + llmKeyEnv + "}\"",
		"export TAVILY_KEY_ENV=\"${TAVILY_KEY_ENV:-" + webKeyEnv + "}\"",
	}, "\n")

	if !strings.Contains(text, "BOT_HOST_DATA_DIR") {
		marker := "PROJECT_DIR=\"$(cd \"$(dirname \"$0\")\" && pwd)\""
		if strings.Contains(text, marker) {
			text = strings.Replace(text, marker, marker+"\n"+injection, 1)
		} else {
			text = injection + "\n" + text
		}
	}

	if hostDataDir != "" {
		text = strings.ReplaceAll(text, "$PROJECT_DIR/.metaclaw", hostDataDir)
	}

	if err := os.WriteFile(path, []byte(text), 0o755); err != nil {
		return fmt.Errorf("write chat.sh: %w", err)
	}
	return nil
}

func rewriteQuickstartRuntimeDefault(path, runtimeTarget string) error {
	runtimeTarget = strings.TrimSpace(runtimeTarget)
	if runtimeTarget == "" {
		return errors.New("runtime target is empty")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read chat.sh: %w", err)
	}
	text := string(b)
	lines := strings.Split(text, "\n")
	replaced := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "export RUNTIME_TARGET=") {
			continue
		}
		lines[i] = "export RUNTIME_TARGET=\"${RUNTIME_TARGET:-" + runtimeTarget + "}\""
		replaced = true
	}
	if !replaced {
		marker := "PROJECT_DIR=\"$(cd \"$(dirname \"$0\")\" && pwd)\""
		injection := "export RUNTIME_TARGET=\"${RUNTIME_TARGET:-" + runtimeTarget + "}\""
		if strings.Contains(text, marker) {
			text = strings.Replace(text, marker, marker+"\n"+injection, 1)
			lines = strings.Split(text, "\n")
		} else {
			lines = append([]string{injection}, lines...)
		}
	}
	out := strings.Join(lines, "\n")
	if err := os.WriteFile(path, []byte(out), 0o755); err != nil {
		return fmt.Errorf("write chat.sh: %w", err)
	}
	return nil
}

func writeObsidianProfileDefaults(path string, profile obsidianProfile) error {
	payload := map[string]string{
		"network_mode":       profile.NetworkMode,
		"render_mode":        profile.RenderMode,
		"retrieval_scope":    profile.RetrievalScope,
		"write_confirm_mode": profile.WriteConfirm,
		"save_default_dir":   profile.SaveDefaultDir,
	}
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal profile defaults: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create defaults dir: %w", err)
	}
	if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
		return fmt.Errorf("write profile defaults: %w", err)
	}
	return nil
}

func writeQuickstartEnvExample(path, llmKeyEnv, webKeyEnv string) error {
	content := strings.Join([]string{
		"# Runtime-only secrets (never commit actual values)",
		llmKeyEnv + "=",
		webKeyEnv + "=",
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write .env.example: %w", err)
	}
	return nil
}

func writeQuickstartGitignore(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	content := strings.Join([]string{
		"# MetaClaw local state",
		".metaclaw/",
		"",
		"# Local secrets (never commit)",
		".env",
		".env.*",
		"",
		"# Python",
		"__pycache__/",
		"*.pyc",
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write .gitignore: %w", err)
	}
	return nil
}

func buildQuickstartImage(projectDir, runtimeTarget, runtimeBin string) error {
	scriptErr := runScript(filepath.Join(projectDir, "build_image.sh"), projectDir, map[string]string{
		"RUNTIME_BIN": runtimeBin,
	}, false)
	if scriptErr == nil {
		return nil
	}
	pinnedRef, recoverErr := recoverQuickstartPinnedImage(projectDir, runtimeTarget, runtimeBin)
	if recoverErr != nil {
		return fmt.Errorf("%w (pin recovery failed: %v)", scriptErr, recoverErr)
	}
	fmt.Fprintf(os.Stderr, "warning: build script exited with %v; recovered runtime.image as %s\n", scriptErr, pinnedRef)
	return nil
}

func recoverQuickstartPinnedImage(projectDir, runtimeTarget, runtimeBin string) (string, error) {
	taggedImage := inferQuickstartTaggedImage(projectDir)
	pinnedRef, err := resolvePinnedImageRef(runtimeTarget, runtimeBin, taggedImage)
	if err != nil {
		return "", err
	}
	if runtimeTarget != "docker" {
		if err := tagPinnedImageRef(runtimeBin, taggedImage, pinnedRef); err != nil {
			return "", fmt.Errorf("tag pinned image: %w", err)
		}
	}
	agentPath := filepath.Join(projectDir, "agent.claw")
	if err := rewriteRuntimeImageRef(agentPath, pinnedRef); err != nil {
		return "", err
	}
	return pinnedRef, nil
}

func inferQuickstartTaggedImage(projectDir string) string {
	repo := strings.TrimSpace(os.Getenv("IMAGE_REPO"))
	tag := strings.TrimSpace(os.Getenv("IMAGE_TAG"))
	if repo != "" || tag != "" {
		if repo == "" {
			repo = quickstartDefaultImageRepo
		}
		if tag == "" {
			tag = quickstartDefaultImageTag
		}
		return repo + ":" + tag
	}

	agentPath := filepath.Join(projectDir, "agent.claw")
	current, err := readRuntimeImageRef(agentPath)
	if err == nil && strings.TrimSpace(current) != "" {
		return strings.SplitN(current, "@", 2)[0]
	}
	return quickstartDefaultImageRepo + ":" + quickstartDefaultImageTag
}

func resolvePinnedImageRef(runtimeTarget, runtimeBin, taggedImage string) (string, error) {
	switch runtimeTarget {
	case "apple_container":
		return resolveApplePinnedImageRef(runtimeBin, taggedImage)
	case "podman", "docker":
		return resolveOCICompatiblePinnedImageRef(runtimeBin, taggedImage)
	default:
		return "", fmt.Errorf("unsupported runtime target for pin recovery: %s", runtimeTarget)
	}
}

func resolveApplePinnedImageRef(runtimeBin, taggedImage string) (string, error) {
	out, err := exec.Command(runtimeBin, "image", "inspect", taggedImage).Output()
	if err != nil {
		return "", fmt.Errorf("inspect image %s: %w", taggedImage, err)
	}
	return parseApplePinnedImageRef(out, taggedImage)
}

func parseApplePinnedImageRef(raw []byte, fallbackImage string) (string, error) {
	type appleIndex struct {
		Digest string `json:"digest"`
	}
	type appleInspect struct {
		Name  string     `json:"name"`
		Index appleIndex `json:"index"`
	}

	var entries []appleInspect
	if err := json.Unmarshal(bytes.TrimSpace(raw), &entries); err != nil {
		return "", fmt.Errorf("parse inspect output: %w", err)
	}
	for _, item := range entries {
		digest := strings.TrimSpace(item.Index.Digest)
		if !strings.HasPrefix(digest, "sha256:") {
			continue
		}
		name := strings.TrimSpace(item.Name)
		if name == "" {
			name = fallbackImage
		}
		name = strings.SplitN(name, "@", 2)[0]
		return name + "@" + digest, nil
	}
	return "", fmt.Errorf("digest not found in inspect output")
}

func resolveOCICompatiblePinnedImageRef(runtimeBin, taggedImage string) (string, error) {
	repoDigestsOut, err := exec.Command(runtimeBin, "image", "inspect", taggedImage, "--format", "{{json .RepoDigests}}").Output()
	if err == nil {
		var repoDigests []string
		if unmarshalErr := json.Unmarshal(bytes.TrimSpace(repoDigestsOut), &repoDigests); unmarshalErr == nil {
			for _, digestRef := range repoDigests {
				digestRef = strings.TrimSpace(digestRef)
				if strings.Contains(digestRef, "@sha256:") {
					return digestRef, nil
				}
			}
		}
	}

	digestOut, digestErr := exec.Command(runtimeBin, "image", "inspect", taggedImage, "--format", "{{.Digest}}").Output()
	if digestErr != nil {
		return "", fmt.Errorf("inspect digest for %s: %w", taggedImage, digestErr)
	}
	digest := strings.TrimSpace(string(digestOut))
	if !strings.HasPrefix(digest, "sha256:") {
		return "", fmt.Errorf("inspect returned empty digest for %s", taggedImage)
	}
	base := strings.SplitN(taggedImage, "@", 2)[0]
	return base + "@" + digest, nil
}

func tagPinnedImageRef(runtimeBin, source, target string) error {
	cmd := exec.Command(runtimeBin, "image", "tag", source, target)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run()
}

func readRuntimeImageRef(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read agent.claw: %w", err)
	}
	for _, line := range strings.Split(string(b), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "image:") {
			value := strings.TrimSpace(strings.TrimPrefix(trimmed, "image:"))
			if value == "" {
				return "", errors.New("runtime.image is empty")
			}
			return value, nil
		}
	}
	return "", errors.New("runtime.image not found")
}

func rewriteRuntimeImageRef(path, pinnedRef string) error {
	if strings.TrimSpace(pinnedRef) == "" {
		return errors.New("pinned runtime image is empty")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read agent.claw: %w", err)
	}
	lines := strings.Split(string(b), "\n")
	replaced := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "image:") {
			continue
		}
		indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		lines[i] = indent + "image: " + pinnedRef
		replaced = true
		break
	}
	if !replaced {
		return errors.New("runtime.image not found in agent.claw")
	}
	out := strings.Join(lines, "\n")
	if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
		return fmt.Errorf("write agent.claw: %w", err)
	}
	return nil
}

func runScript(scriptPath, dir string, extraEnv map[string]string, interactive bool) error {
	cmd := exec.Command(scriptPath)
	cmd.Dir = dir
	cmd.Env = mergedEnv(extraEnv)
	if interactive {
		cmd.Stdin = os.Stdin
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func mergedEnv(extra map[string]string) []string {
	if len(extra) == 0 {
		return os.Environ()
	}
	m := map[string]string{}
	for _, item := range os.Environ() {
		if i := strings.IndexByte(item, '='); i > 0 {
			m[item[:i]] = item[i+1:]
		}
	}
	for k, v := range extra {
		if strings.TrimSpace(k) == "" {
			continue
		}
		m[k] = v
	}
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	sort.Strings(out)
	return out
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// checkDiskSpace verifies adequate disk space is available for rafikiclaw operations.
func checkDiskSpace(minMB int, add func(name, status, detail string)) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		add("disk_space", doctorStatusWarn, "cannot determine home directory for space check")
		return
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(homeDir, &stat); err != nil {
		add("disk_space", doctorStatusWarn, fmt.Sprintf("cannot statfs %s: %v", homeDir, err))
		return
	}
	freeBytes := stat.Bfree * uint64(stat.Bsize)
	freeMB := int(freeBytes / 1024 / 1024)
	if freeMB < minMB {
		add("disk_space", doctorStatusFail, fmt.Sprintf("only %d MB free (minimum %d MB required for safe operation)", freeMB, minMB))
		return
	}
	add("disk_space", doctorStatusPass, fmt.Sprintf("%d MB free (above %d MB threshold)", freeMB, minMB))
}

// checkNetworkConnectivity verifies outbound network access.
// rafikiclaw does NOT require internet for local-first operation, but reports connectivity status.
func checkNetworkConnectivity(add func(name, status, detail string)) {
	// Try reach both Docker Hub (container images) and GitHub (examples/upgrades) to give useful signal.
	targets := []struct {
		host  string
		label string
	}{
		{"registry-1.docker.io", "docker hub"},
		{"github.com", "github.com"},
	}
	for _, t := range targets {
		address := t.host + ":443"
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		dialer := &net.Dialer{}
		conn, err := dialer.DialContext(ctx, "tcp", address)
		cancel()
		if err != nil {
			add("network", doctorStatusWarn, fmt.Sprintf("cannot reach %s (%s): %v", t.label, t.host, err))
			return
		}
		_ = conn.Close()
	}
	add("network", doctorStatusPass, "outbound connectivity OK (docker hub + github.com reachable)")
}
