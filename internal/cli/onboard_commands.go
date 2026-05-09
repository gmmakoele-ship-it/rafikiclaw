package cli

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/term"
)

type onboardOptions struct {
	ProjectDir string
	VaultPath  string
	VaultWrite bool
	Runtime    string
	Profile    string
	LLMKeyEnv  string
	WebKeyEnv  string

	SkipBuild bool
	NoRun     bool
	Force     bool

	InteractiveExplicit bool
	SaveEnv             bool
}

func runOnboard(args []string) int {
	rawArgs := append([]string(nil), args...)

	args = reorderFlags(args, map[string]bool{
		"--project-dir": true,
		"--vault":       true,
		"--vault-write": false,
		"--runtime":     true,
		"--profile":     true,
		"--llm-key-env": true,
		"--web-key-env": true,
		"--interactive": false,
		"--save-env":    false,
		"--skip-build":  false,
		"--no-run":      false,
		"--force":       false,
	})

	fs := flag.NewFlagSet("onboard", flag.ContinueOnError)
	opts := onboardOptions{
		ProjectDir: "./my-obsidian-bot",
		Runtime:    "auto",
		Profile:    "obsidian-chat",
		LLMKeyEnv:  "OPENAI_FORMAT_API_KEY",
		WebKeyEnv:  "TAVILY_API_KEY",
		SaveEnv:    true,
	}
	fs.StringVar(&opts.ProjectDir, "project-dir", opts.ProjectDir, "project directory (default ./my-obsidian-bot)")
	fs.StringVar(&opts.VaultPath, "vault", "", "absolute vault path (interactive prompt if omitted)")
	fs.BoolVar(&opts.VaultWrite, "vault-write", false, "mount vault read-write inside container (less safe; default is read-only)")
	fs.StringVar(&opts.Runtime, "runtime", opts.Runtime, "runtime target (auto|apple_container|podman|docker)")
	fs.StringVar(&opts.Profile, "profile", opts.Profile, "profile (obsidian-chat|obsidian-research)")
	fs.StringVar(&opts.LLMKeyEnv, "llm-key-env", opts.LLMKeyEnv, "LLM API key env name (default OPENAI_FORMAT_API_KEY)")
	fs.StringVar(&opts.WebKeyEnv, "web-key-env", opts.WebKeyEnv, "web search API key env name (default TAVILY_API_KEY)")
	fs.BoolVar(&opts.InteractiveExplicit, "interactive", false, "run interactive step-by-step onboarding")
	fs.BoolVar(&opts.SaveEnv, "save-env", opts.SaveEnv, "write keys into <project>/.env for convenience (gitignored)")
	fs.BoolVar(&opts.SkipBuild, "skip-build", false, "skip image build")
	fs.BoolVar(&opts.NoRun, "no-run", false, "prepare project only, do not launch chat")
	fs.BoolVar(&opts.Force, "force", false, "allow using a non-empty project directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	remaining := fs.Args()
	if len(remaining) != 1 || remaining[0] != "obsidian" {
		fmt.Fprintln(os.Stderr, "usage: rafikiclaw onboard obsidian [--interactive] [--project-dir=./my-obsidian-bot] [--vault=/abs/path/to/vault] [--vault-write] [--runtime=auto|apple_container|podman|docker] [--profile=obsidian-chat] [--save-env] [--skip-build] [--no-run] [--force]")
		return 1
	}

	modeInteractive := opts.InteractiveExplicit || (len(rawArgs) == 1 && rawArgs[0] == "obsidian")
	if modeInteractive {
		if !isInteractiveTerminal() {
			fmt.Fprintln(os.Stderr, "onboard failed: interactive prompts require a TTY (pass flags instead)")
			return 1
		}
		var err error
		opts, err = collectOnboardInteractiveOptions(opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "onboard failed: %v\n", err)
			return 1
		}
	} else {
		if strings.TrimSpace(opts.VaultPath) == "" {
			fmt.Fprintln(os.Stderr, "onboard failed: --vault is required in non-interactive mode")
			return 1
		}
	}

	opts.LLMKeyEnv = strings.TrimSpace(opts.LLMKeyEnv)
	if opts.LLMKeyEnv == "" {
		opts.LLMKeyEnv = "OPENAI_FORMAT_API_KEY"
	}
	if !wizardEnvNameRef.MatchString(opts.LLMKeyEnv) {
		fmt.Fprintln(os.Stderr, "onboard failed: --llm-key-env must be a valid environment variable name")
		return 1
	}
	opts.WebKeyEnv = strings.TrimSpace(opts.WebKeyEnv)
	if opts.WebKeyEnv == "" {
		opts.WebKeyEnv = "TAVILY_API_KEY"
	}
	if !wizardEnvNameRef.MatchString(opts.WebKeyEnv) {
		fmt.Fprintln(os.Stderr, "onboard failed: --web-key-env must be a valid environment variable name")
		return 1
	}

	var err error
	opts.ProjectDir, err = filepath.Abs(strings.TrimSpace(opts.ProjectDir))
	if err != nil {
		fmt.Fprintf(os.Stderr, "onboard failed: resolve project dir: %v\n", err)
		return 1
	}
	opts.VaultPath, err = filepath.Abs(strings.TrimSpace(opts.VaultPath))
	if err != nil {
		fmt.Fprintf(os.Stderr, "onboard failed: resolve vault path: %v\n", err)
		return 1
	}

	// Safety/UX: keep bot project state outside the vault to avoid clutter and overlapping mount surprises.
	// Interactive flow handles this immediately after the project-dir prompt; we keep warnings here for non-interactive usage.
	if !modeInteractive {
		if isSubpath(opts.ProjectDir, opts.VaultPath) {
			fmt.Fprintf(os.Stderr, "warning: project directory is inside your vault (%s). Recommended: keep them separate.\n", opts.VaultPath)
		}
	}

	// Ensure an LLM key exists (either already in env or entered interactively).
	if strings.TrimSpace(os.Getenv(opts.LLMKeyEnv)) == "" {
		if modeInteractive {
			key, err := promptSecret(os.Stderr, fmt.Sprintf("Enter %s (hidden input): ", opts.LLMKeyEnv))
			if err != nil {
				fmt.Fprintf(os.Stderr, "onboard failed: read key: %v\n", err)
				return 1
			}
			key = strings.TrimSpace(key)
			if key == "" {
				fmt.Fprintf(os.Stderr, "onboard failed: %s cannot be empty\n", opts.LLMKeyEnv)
				return 1
			}
			_ = os.Setenv(opts.LLMKeyEnv, key)
		} else {
			fmt.Fprintf(os.Stderr, "onboard failed: missing LLM key (export %s=...)\n", opts.LLMKeyEnv)
			return 1
		}
	}

	// Prepare project via quickstart (always --no-run so we can optionally write .env first).
	quickArgs := []string{
		"obsidian",
		"--project-dir", opts.ProjectDir,
		"--vault", opts.VaultPath,
		"--runtime", strings.TrimSpace(opts.Runtime),
		"--profile", strings.TrimSpace(opts.Profile),
		"--llm-key-env", opts.LLMKeyEnv,
		"--web-key-env", opts.WebKeyEnv,
		"--no-run",
	}
	if opts.VaultWrite {
		quickArgs = append(quickArgs, "--vault-write")
	}
	if opts.SkipBuild {
		quickArgs = append(quickArgs, "--skip-build")
	}
	if opts.Force {
		quickArgs = append(quickArgs, "--force")
	}
	if rc := runQuickstart(quickArgs); rc != 0 {
		return rc
	}

	if opts.SaveEnv {
		env := map[string]string{}
		env[opts.LLMKeyEnv] = strings.TrimSpace(os.Getenv(opts.LLMKeyEnv))
		if strings.TrimSpace(os.Getenv(opts.WebKeyEnv)) != "" {
			env[opts.WebKeyEnv] = strings.TrimSpace(os.Getenv(opts.WebKeyEnv))
		}
		if err := writeDotEnvFile(filepath.Join(opts.ProjectDir, ".env"), env); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to write .env: %v\n", err)
		}
	}

	if opts.NoRun {
		return 0
	}

	exePath := "rafikiclaw"
	if exe, err := os.Executable(); err == nil {
		exePath = exe
	}
	fmt.Println("launching chat...")
	if err := runScript(filepath.Join(opts.ProjectDir, "chat.sh"), opts.ProjectDir, map[string]string{
		"METACLAW_BIN": exePath,
	}, true); err != nil {
		fmt.Fprintf(os.Stderr, "onboard failed: chat.sh: %v\n", err)
		return 1
	}
	return 0
}

func isSubpath(child, parent string) bool {
	child = filepath.Clean(strings.TrimSpace(child))
	parent = filepath.Clean(strings.TrimSpace(parent))
	if child == "" || parent == "" {
		return false
	}
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	rel = filepath.Clean(rel)
	return rel == "." || (!strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && rel != "..")
}

func collectOnboardInteractiveOptions(in onboardOptions) (onboardOptions, error) {
	reader := bufio.NewReader(os.Stdin)
	var err error

	// Ask for a project directory first. This becomes the bot project directory.
	// Then ask for an Obsidian vault path with a sensible default under the project directory,
	// while still allowing users to point at an existing vault elsewhere.
	// Intentionally no default: users should explicitly choose where the project directory lives.
	workDir, err := promptLine(reader, os.Stderr, "Project directory", "")
	if err != nil {
		return in, err
	}
	workAbs, err := filepath.Abs(strings.TrimSpace(expandLeadingTilde(workDir)))
	if err != nil {
		return in, fmt.Errorf("resolve project directory: %w", err)
	}
	in.ProjectDir = workAbs

	// Vault path: default under the project directory, but allow an absolute path elsewhere.
	vaultDefault := strings.TrimSpace(in.VaultPath)
	if vaultDefault == "" {
		vaultDefault = filepath.Join(workAbs, "vault")
	}
	var vaultAbs string
	for {
		vaultInput, err := promptLine(reader, os.Stderr, "Obsidian vault path", vaultDefault)
		if err != nil {
			return in, err
		}
		vaultAbs, err = resolvePathFromDir(workAbs, expandLeadingTilde(vaultInput))
		if err != nil {
			fmt.Fprintf(os.Stderr, "vault path: %v\n", err)
			continue
		}
		if vaultAbs == workAbs {
			fmt.Fprintln(os.Stderr, "vault path must be different from the project directory")
			continue
		}

		// Ensure vault exists for quickstart (any folder can be an Obsidian vault).
		if st, err := os.Stat(vaultAbs); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				ok, err := promptSelectBool(os.Stderr, fmt.Sprintf("Create vault directory now? (%s)", vaultAbs), true)
				if err != nil {
					return in, err
				}
				if !ok {
					continue
				}
				if err := os.MkdirAll(vaultAbs, 0o755); err != nil {
					fmt.Fprintf(os.Stderr, "vault directory: create failed: %v\n", err)
					continue
				}
			} else {
				fmt.Fprintf(os.Stderr, "vault directory: not accessible: %v\n", err)
				continue
			}
		} else if !st.IsDir() {
			fmt.Fprintln(os.Stderr, "vault directory: path exists but is not a directory")
			continue
		}

		break
	}
	in.VaultPath = vaultAbs

	// Optional warning: putting the project inside the vault can clutter notes and creates overlapping host paths.
	// (The default flow keeps the vault under the project, which is fine.)
	if isSubpath(workAbs, vaultAbs) && workAbs != vaultAbs {
		fmt.Fprintf(os.Stderr, "warning: project directory is inside your vault (%s). Recommended: keep them separate.\n", vaultAbs)
	}

	vaultAccess, err := promptSelect(os.Stderr, "Vault access", []string{"read-only (recommended)", "read-write (less safe)"}, "read-only (recommended)")
	if err != nil {
		return in, err
	}
	in.VaultWrite = strings.HasPrefix(vaultAccess, "read-write")

	runtime, err := promptSelect(os.Stderr, "Runtime target", []string{"auto", "apple_container", "podman", "docker"}, in.Runtime)
	if err != nil {
		return in, err
	}
	in.Runtime = runtime

	profileOptions := []string{"obsidian-chat", "obsidian-research"}
	profile, err := promptSelect(os.Stderr, "Profile", profileOptions, in.Profile)
	if err != nil {
		return in, err
	}
	in.Profile = profile

	saveEnv, err := promptSelectBool(os.Stderr, "Save keys you enter today into <project>/.env (gitignored)?", in.SaveEnv)
	if err != nil {
		return in, err
	}
	in.SaveEnv = saveEnv

	needWebDefault := strings.TrimSpace(in.Profile) == "obsidian-research"
	needWeb, err := promptSelectBool(os.Stderr, "Enable web search (optional, requires key)?", needWebDefault)
	if err != nil {
		return in, err
	}
	if needWeb && strings.TrimSpace(os.Getenv(in.WebKeyEnv)) == "" {
		key, err := promptSecret(os.Stderr, fmt.Sprintf("Enter %s (hidden input): ", in.WebKeyEnv))
		if err != nil {
			return in, err
		}
		key = strings.TrimSpace(key)
		if key != "" {
			_ = os.Setenv(in.WebKeyEnv, key)
		}
	}

	launch, err := promptSelectBool(os.Stderr, "Launch chat now?", !in.NoRun)
	if err != nil {
		return in, err
	}
	in.NoRun = !launch

	return in, nil
}

func resolvePathFromDir(baseAbs, userInput string) (string, error) {
	baseAbs = filepath.Clean(strings.TrimSpace(baseAbs))
	if baseAbs == "" {
		return "", errors.New("missing project directory")
	}
	value := stripOuterQuotes(strings.TrimSpace(userInput))
	if value == "" {
		return "", errors.New("value is required")
	}
	value = expandLeadingTilde(value)

	var abs string
	var err error
	if filepath.IsAbs(value) {
		abs, err = filepath.Abs(value)
		if err != nil {
			return "", err
		}
	} else {
		abs, err = filepath.Abs(filepath.Join(baseAbs, value))
		if err != nil {
			return "", err
		}
	}

	return abs, nil
}

func expandLeadingTilde(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value[0] != '~' {
		return value
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return value
	}
	if value == "~" {
		return home
	}
	if strings.HasPrefix(value, "~"+string(os.PathSeparator)) {
		return filepath.Join(home, value[2:])
	}
	if strings.HasPrefix(value, "~/") {
		return filepath.Join(home, value[2:])
	}
	return value
}

func promptLine(r *bufio.Reader, w *os.File, label, defaultValue string) (string, error) {
	for {
		if strings.TrimSpace(defaultValue) != "" {
			fmt.Fprintf(w, "%s [%s]: ", label, defaultValue)
		} else {
			fmt.Fprintf(w, "%s: ", label)
		}
		line, err := r.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
		value := strings.TrimSpace(line)
		if value == "" {
			value = strings.TrimSpace(defaultValue)
		}
		value = stripOuterQuotes(value)
		if value != "" {
			return value, nil
		}
		if errors.Is(err, io.EOF) {
			return "", errors.New("input closed before value was provided")
		}
		fmt.Fprintln(w, "value is required")
	}
}

func stripOuterQuotes(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 {
		if (value[0] == '\'' && value[len(value)-1] == '\'') || (value[0] == '"' && value[len(value)-1] == '"') {
			return strings.TrimSpace(value[1 : len(value)-1])
		}
	}
	return value
}

func promptSelectBool(w *os.File, label string, defaultYes bool) (bool, error) {
	defaultValue := "no"
	if defaultYes {
		defaultValue = "yes"
	}
	v, err := promptSelect(w, label, []string{"yes", "no"}, defaultValue)
	if err != nil {
		return false, err
	}
	return v == "yes", nil
}

func promptSelect(w *os.File, label string, options []string, defaultValue string) (string, error) {
	if !isInteractiveTerminal() || !term.IsTerminal(int(os.Stdin.Fd())) {
		// Fall back to a plain line prompt when no TTY is available.
		reader := bufio.NewReader(os.Stdin)
		return promptLine(reader, w, label, defaultValue)
	}
	if len(options) == 0 {
		return "", errors.New("no options available")
	}

	selected := 0
	defaultValue = strings.TrimSpace(defaultValue)
	if defaultValue != "" {
		for i, opt := range options {
			if strings.EqualFold(opt, defaultValue) {
				selected = i
				break
			}
		}
	}

	fd := int(os.Stdin.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return "", err
	}
	defer term.Restore(fd, oldState)

	// Hide cursor while selecting.
	fmt.Fprint(w, "\x1b[?25l")
	defer fmt.Fprint(w, "\x1b[?25h")

	crlf := func() {
		// In raw mode, the terminal won't translate '\n' to CRLF automatically.
		fmt.Fprint(w, "\r\n")
	}
	printLine := func(s string) {
		// Ensure we fully clear the line before printing (prevents odd "staircase" rendering on some terminals).
		fmt.Fprint(w, "\r\x1b[2K")
		fmt.Fprint(w, s)
		crlf()
	}

	lines := len(options) + 1

	render := func() {
		printLine(label + " (use ↑/↓, Enter):")
		for i, opt := range options {
			prefix := "  "
			if i == selected {
				prefix = "> "
			}
			printLine(prefix + opt)
		}
	}
	clearMenu := func() {
		// Cursor is currently after the menu; move up to the prompt and clear everything below it.
		fmt.Fprintf(w, "\x1b[%dA\r\x1b[J", lines)
	}
	redraw := func() {
		clearMenu()
		render()
	}

	render()

	readByte := func() (byte, error) {
		var b [1]byte
		_, err := os.Stdin.Read(b[:])
		return b[0], err
	}
	for {
		b, err := readByte()
		if err != nil {
			return "", err
		}

		switch b {
		case '\r', '\n':
			// Clear menu and print the chosen value on one line for transcript readability.
			clearMenu()
			printLine(label + ": " + options[selected])
			return options[selected], nil
		case 0x1b:
			// Escape or arrow key sequence.
			b2, err := readByte()
			if err != nil {
				return "", err
			}
			if b2 != '[' {
				return "", errors.New("selection cancelled")
			}
			b3, err := readByte()
			if err != nil {
				return "", err
			}
			switch b3 {
			case 'A': // up
				if selected == 0 {
					selected = len(options) - 1
				} else {
					selected--
				}
				redraw()
			case 'B': // down
				selected = (selected + 1) % len(options)
				redraw()
			case 'C', 'D':
				// ignore left/right
			default:
				// ignore other sequences
			}
		case 'k', 'K':
			if selected == 0 {
				selected = len(options) - 1
			} else {
				selected--
			}
			redraw()
		case 'j', 'J':
			selected = (selected + 1) % len(options)
			redraw()
		case 'q', 'Q':
			return "", errors.New("selection cancelled")
		default:
			// ignore
		}
	}
}

func promptSecret(w *os.File, prompt string) (string, error) {
	fmt.Fprint(w, prompt)
	if isInteractiveTerminal() && term.IsTerminal(int(os.Stdin.Fd())) {
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(w)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func writeDotEnvFile(path string, env map[string]string) error {
	if len(env) == 0 {
		return nil
	}
	if st, err := os.Stat(path); err == nil && st.Size() > 0 {
		// Respect existing .env to avoid surprising overwrites.
		return nil
	}
	lines := []string{
		"# Runtime-only secrets (never commit actual values)",
	}
	keys := []string{}
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := env[k]
		if strings.ContainsAny(v, "\n\r") {
			return fmt.Errorf("invalid value for %s (contains newline)", k)
		}
		lines = append(lines, k+"="+v)
	}
	lines = append(lines, "")
	content := strings.Join(lines, "\n")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return err
	}
	return nil
}
