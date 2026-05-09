package cli

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	v1 "github.com/gmmakoele-ship-it/rafikiclaw/internal/claw/schema/v1"
	"gopkg.in/yaml.v3"
)

var wizardEnvNameRef = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type wizardOptions struct {
	ProjectDir          string
	OutputPath          string
	AgentName           string
	VaultPath           string
	ConfigPath          string
	LogsPath            string
	ReadOnlyVault       bool
	NetworkMode         string
	Lifecycle           v1.LifecycleMode
	RuntimeTarget       v1.RuntimeTarget
	LLMEnabled          bool
	LLMProvider         v1.LLMProvider
	LLMModel            string
	LLMBaseURL          string
	LLMAPIKeyEnv        string
	DefaultImage        string
	LLMFlagDisabled     bool
	InteractiveExplicit bool
}

func runWizard(args []string) int {
	rawArgs := append([]string(nil), args...)

	args = reorderFlags(args, map[string]bool{
		"--project-dir":   true,
		"--out":           true,
		"--agent-name":    true,
		"--vault":         true,
		"--config-dir":    true,
		"--logs-dir":      true,
		"--network":       true,
		"--lifecycle":     true,
		"--runtime":       true,
		"--provider":      true,
		"--model":         true,
		"--base-url":      true,
		"--api-key-env":   true,
		"--read-only":     false,
		"--llm-disabled":  false,
		"--species-image": true,
		"--interactive":   false,
	})

	fs := flag.NewFlagSet("wizard", flag.ContinueOnError)
	opts := wizardOptions{
		OutputPath:    "obsidian-bot.claw",
		AgentName:     "obsidian-research-bot",
		NetworkMode:   "outbound",
		Lifecycle:     v1.LifecycleDaemon,
		LLMEnabled:    true,
		LLMProvider:   v1.LLMProviderGeminiOpenAI,
		LLMModel:      "gemini-2.5-pro",
		LLMBaseURL:    "https://generativelanguage.googleapis.com/v1beta/openai/",
		LLMAPIKeyEnv:  "GEMINI_API_KEY",
		DefaultImage:  defaultWizardImage(),
		RuntimeTarget: "",
	}
	fs.StringVar(&opts.ProjectDir, "project-dir", opts.ProjectDir, "project root directory (creates isolated vault/config/logs layout)")
	fs.StringVar(&opts.OutputPath, "out", opts.OutputPath, "output clawfile path")
	fs.StringVar(&opts.AgentName, "agent-name", opts.AgentName, "agent name")
	fs.StringVar(&opts.VaultPath, "vault", opts.VaultPath, "Obsidian vault path on host")
	fs.StringVar(&opts.ConfigPath, "config-dir", opts.ConfigPath, "config directory mount on host")
	fs.StringVar(&opts.LogsPath, "logs-dir", opts.LogsPath, "logs directory mount on host")
	fs.BoolVar(&opts.ReadOnlyVault, "read-only", false, "mount vault as read-only")
	fs.StringVar(&opts.NetworkMode, "network", opts.NetworkMode, "network mode (none|outbound|all)")
	lifecycle := string(opts.Lifecycle)
	fs.StringVar(&lifecycle, "lifecycle", lifecycle, "agent lifecycle (ephemeral|daemon|debug)")
	runtimeTarget := string(opts.RuntimeTarget)
	fs.StringVar(&runtimeTarget, "runtime", runtimeTarget, "runtime target override in clawfile (podman|apple_container|docker)")
	provider := string(opts.LLMProvider)
	fs.StringVar(&provider, "provider", provider, "llm provider (gemini_openai|openai_compatible|none)")
	fs.StringVar(&opts.LLMModel, "model", opts.LLMModel, "llm model name")
	fs.StringVar(&opts.LLMBaseURL, "base-url", opts.LLMBaseURL, "llm base URL (optional; for openai_compatible endpoints)")
	fs.StringVar(&opts.LLMAPIKeyEnv, "api-key-env", opts.LLMAPIKeyEnv, "host env variable used for runtime key injection")
	fs.BoolVar(&opts.LLMFlagDisabled, "llm-disabled", false, "disable llm contract in scaffold")
	fs.StringVar(&opts.DefaultImage, "species-image", opts.DefaultImage, "runtime image (must be digest-pinned)")
	fs.BoolVar(&opts.InteractiveExplicit, "interactive", false, "run interactive step-by-step wizard")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if len(fs.Args()) != 0 {
		fmt.Fprintln(os.Stderr, "usage: metaclaw wizard [--interactive] [--project-dir=./my-bot] [--out=agent.claw] [--vault=./vault] [--provider=gemini_openai]")
		return 1
	}

	modeInteractive := len(rawArgs) == 0 || opts.InteractiveExplicit
	if modeInteractive {
		var err error
		opts, err = collectWizardInteractiveOptions(opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "wizard failed: %v\n", err)
			return 1
		}
		lifecycle = string(opts.Lifecycle)
		runtimeTarget = string(opts.RuntimeTarget)
		if opts.LLMEnabled {
			provider = string(opts.LLMProvider)
		} else {
			provider = "none"
		}
		rawArgs = []string{"--interactive", "--project-dir"}
	}

	if err := resolveWizardPaths(&opts, wizardPathInputs{
		OutProvided: hasFlagToken(rawArgs, "--out", "-out"),
	}); err != nil {
		fmt.Fprintf(os.Stderr, "wizard failed: %v\n", err)
		return 1
	}

	opts.AgentName = strings.TrimSpace(opts.AgentName)
	if opts.AgentName == "" {
		fmt.Fprintln(os.Stderr, "wizard failed: --agent-name cannot be empty")
		return 1
	}
	opts.NetworkMode = strings.TrimSpace(opts.NetworkMode)
	if err := validateWizardNetwork(opts.NetworkMode); err != nil {
		fmt.Fprintf(os.Stderr, "wizard failed: %v\n", err)
		return 1
	}
	opts.Lifecycle = v1.LifecycleMode(strings.TrimSpace(lifecycle))
	if !opts.Lifecycle.Valid() {
		fmt.Fprintln(os.Stderr, "wizard failed: --lifecycle must be ephemeral|daemon|debug")
		return 1
	}
	opts.RuntimeTarget = v1.RuntimeTarget(strings.TrimSpace(runtimeTarget))
	if !opts.RuntimeTarget.Valid() {
		fmt.Fprintln(os.Stderr, "wizard failed: --runtime must be podman|apple_container|docker")
		return 1
	}
	opts.LLMEnabled = !opts.LLMFlagDisabled
	if err := normalizeWizardLLM(&opts, provider); err != nil {
		fmt.Fprintf(os.Stderr, "wizard failed: %v\n", err)
		return 1
	}
	if err := materializeWizardLayout(opts); err != nil {
		fmt.Fprintf(os.Stderr, "wizard failed: %v\n", err)
		return 1
	}

	cfg := buildWizardClawfile(opts)
	content, err := renderWizardClawfile(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wizard failed: render clawfile: %v\n", err)
		return 1
	}
	if err := os.WriteFile(opts.OutputPath, content, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "wizard failed: write output: %v\n", err)
		return 1
	}

	fmt.Printf("created %s\n", opts.OutputPath)
	if opts.ProjectDir != "" {
		fmt.Printf("project dir: %s\n", opts.ProjectDir)
	}
	fmt.Printf("vault mount: %s -> /vault (readOnly=%v)\n", opts.VaultPath, opts.ReadOnlyVault)
	fmt.Printf("config mount: %s -> /config (readOnly=true)\n", opts.ConfigPath)
	fmt.Printf("logs mount: %s -> /logs\n", opts.LogsPath)
	if opts.LLMEnabled {
		fmt.Printf("llm contract: provider=%s model=%s apiKeyEnv=%s\n", opts.LLMProvider, opts.LLMModel, opts.LLMAPIKeyEnv)
		fmt.Printf("next: export %s=<your_api_key>\n", opts.LLMAPIKeyEnv)
		fmt.Printf("next: metaclaw run %s --llm-api-key-env=%s\n", opts.OutputPath, opts.LLMAPIKeyEnv)
	} else {
		fmt.Println("llm contract: disabled")
		fmt.Printf("next: metaclaw run %s\n", opts.OutputPath)
	}
	return 0
}

func defaultWizardImage() string {
	profile, ok := v1.SpeciesProfileFor(v1.SpeciesMicro)
	if !ok {
		return ""
	}
	return profile.DefaultImage
}

type wizardPathInputs struct {
	OutProvided bool
}

func resolveWizardPaths(opts *wizardOptions, in wizardPathInputs) error {
	if opts == nil {
		return fmt.Errorf("internal error: nil wizard options")
	}
	opts.ProjectDir = strings.TrimSpace(opts.ProjectDir)
	opts.OutputPath = strings.TrimSpace(opts.OutputPath)
	opts.VaultPath = strings.TrimSpace(opts.VaultPath)
	opts.ConfigPath = strings.TrimSpace(opts.ConfigPath)
	opts.LogsPath = strings.TrimSpace(opts.LogsPath)

	var err error
	baseDir := ""
	if opts.ProjectDir != "" {
		opts.ProjectDir, err = filepath.Abs(opts.ProjectDir)
		if err != nil {
			return fmt.Errorf("resolve project dir: %w", err)
		}
		baseDir = opts.ProjectDir
	}

	if opts.OutputPath == "" {
		if baseDir != "" {
			opts.OutputPath = filepath.Join(baseDir, "agent.claw")
		} else {
			opts.OutputPath = "obsidian-bot.claw"
		}
	}
	opts.OutputPath, err = filepath.Abs(opts.OutputPath)
	if err != nil {
		return fmt.Errorf("resolve output path: %w", err)
	}
	if baseDir == "" {
		baseDir = filepath.Dir(opts.OutputPath)
	}
	if baseDir == "." || baseDir == "" {
		baseDir, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("resolve working directory: %w", err)
		}
	}
	if opts.ProjectDir != "" && !in.OutProvided {
		opts.OutputPath = filepath.Join(opts.ProjectDir, "agent.claw")
	}

	if opts.VaultPath == "" {
		opts.VaultPath = filepath.Join(baseDir, "vault")
	}
	if opts.ConfigPath == "" {
		opts.ConfigPath = filepath.Join(baseDir, "config")
	}
	if opts.LogsPath == "" {
		opts.LogsPath = filepath.Join(baseDir, "logs")
	}

	opts.VaultPath, err = filepath.Abs(opts.VaultPath)
	if err != nil {
		return fmt.Errorf("resolve vault path: %w", err)
	}
	opts.ConfigPath, err = filepath.Abs(opts.ConfigPath)
	if err != nil {
		return fmt.Errorf("resolve config path: %w", err)
	}
	opts.LogsPath, err = filepath.Abs(opts.LogsPath)
	if err != nil {
		return fmt.Errorf("resolve logs path: %w", err)
	}
	return nil
}

func materializeWizardLayout(opts wizardOptions) error {
	if opts.ProjectDir != "" {
		if err := os.MkdirAll(opts.ProjectDir, 0o755); err != nil {
			return fmt.Errorf("create project dir: %w", err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(opts.OutputPath), 0o755); err != nil {
		return fmt.Errorf("create output parent: %w", err)
	}
	if err := os.MkdirAll(opts.VaultPath, 0o755); err != nil {
		return fmt.Errorf("create vault path: %w", err)
	}
	if err := os.MkdirAll(opts.ConfigPath, 0o755); err != nil {
		return fmt.Errorf("create config path: %w", err)
	}
	if err := os.MkdirAll(opts.LogsPath, 0o755); err != nil {
		return fmt.Errorf("create logs path: %w", err)
	}
	return nil
}

func validateWizardNetwork(mode string) error {
	switch mode {
	case "none", "outbound", "all":
		return nil
	default:
		return fmt.Errorf("--network must be none|outbound|all")
	}
}

func normalizeWizardLLM(opts *wizardOptions, providerRaw string) error {
	if opts == nil {
		return fmt.Errorf("internal error: nil wizard options")
	}
	if !opts.LLMEnabled {
		opts.LLMProvider = ""
		opts.LLMModel = ""
		opts.LLMBaseURL = ""
		opts.LLMAPIKeyEnv = ""
		return nil
	}

	provider := v1.LLMProvider(strings.TrimSpace(providerRaw))
	if provider == "none" {
		opts.LLMEnabled = false
		opts.LLMProvider = ""
		opts.LLMModel = ""
		opts.LLMBaseURL = ""
		opts.LLMAPIKeyEnv = ""
		return nil
	}
	if !provider.Valid() || provider == "" {
		return fmt.Errorf("--provider must be gemini_openai|openai_compatible|none")
	}
	opts.LLMProvider = provider

	opts.LLMModel = strings.TrimSpace(opts.LLMModel)
	if opts.LLMModel == "" {
		return fmt.Errorf("--model cannot be empty when llm is enabled")
	}
	opts.LLMBaseURL = strings.TrimSpace(opts.LLMBaseURL)
	opts.LLMAPIKeyEnv = strings.TrimSpace(opts.LLMAPIKeyEnv)

	switch provider {
	case v1.LLMProviderGeminiOpenAI:
		if opts.LLMBaseURL == "" {
			opts.LLMBaseURL = "https://generativelanguage.googleapis.com/v1beta/openai/"
		}
		if opts.LLMAPIKeyEnv == "" {
			opts.LLMAPIKeyEnv = "GEMINI_API_KEY"
		}
	case v1.LLMProviderOpenAICompatible:
		if opts.LLMAPIKeyEnv == "" {
			opts.LLMAPIKeyEnv = "OPENAI_API_KEY"
		}
	}
	if !wizardEnvNameRef.MatchString(opts.LLMAPIKeyEnv) {
		return fmt.Errorf("--api-key-env must be a valid environment variable name")
	}
	return nil
}

func buildWizardClawfile(opts wizardOptions) v1.Clawfile {
	mounts := []v1.MountSpec{
		{
			Source:   opts.VaultPath,
			Target:   "/vault",
			ReadOnly: opts.ReadOnlyVault,
		},
		{
			Source:   opts.ConfigPath,
			Target:   "/config",
			ReadOnly: true,
		},
		{
			Source:   opts.LogsPath,
			Target:   "/logs",
			ReadOnly: false,
		},
	}
	cfg := v1.Clawfile{
		APIVersion: "metaclaw/v1",
		Kind:       "Agent",
		Agent: v1.AgentSpec{
			Name:      opts.AgentName,
			Species:   v1.SpeciesMicro,
			Lifecycle: opts.Lifecycle,
			Habitat: v1.HabitatSpec{
				Network: v1.NetworkSpec{Mode: opts.NetworkMode},
				Mounts:  mounts,
				Env: map[string]string{
					"OBSIDIAN_VAULT_DIR":  "/vault",
					"METACLAW_CONFIG_DIR": "/config",
					"METACLAW_LOG_DIR":    "/logs",
				},
			},
			Runtime: v1.RuntimeSpec{
				Image: opts.DefaultImage,
			},
			Command: []string{"sh", "-lc", wizardShellScript(opts)},
		},
	}
	if opts.RuntimeTarget != "" {
		cfg.Agent.Runtime.Target = opts.RuntimeTarget
	}
	if opts.LLMEnabled {
		cfg.Agent.LLM = v1.LLMSpec{
			Provider:  opts.LLMProvider,
			Model:     opts.LLMModel,
			BaseURL:   opts.LLMBaseURL,
			APIKeyEnv: opts.LLMAPIKeyEnv,
		}
	}
	return cfg
}

func wizardShellScript(opts wizardOptions) string {
	if opts.Lifecycle == v1.LifecycleDaemon {
		return `echo "MetaClaw Obsidian daemon scaffold started"
LOG_PATH="${METACLAW_LOG_DIR:-/logs}/metaclaw_bot.log"
echo "vault=${OBSIDIAN_VAULT_DIR:-/vault} config=${METACLAW_CONFIG_DIR:-/config} provider=${METACLAW_LLM_PROVIDER:-none} model=${METACLAW_LLM_MODEL:-none}" | tee -a "$LOG_PATH"
# Replace this loop with your bot runtime (OpenAI-compatible SDK, custom binary, or script).
while true; do
  date -u +"%Y-%m-%dT%H:%M:%SZ heartbeat" | tee -a "$LOG_PATH"
  sleep 60
done
`
	}
	return `echo "MetaClaw Obsidian one-off scaffold started"
LOG_PATH="${METACLAW_LOG_DIR:-/logs}/metaclaw_bot.log"
echo "vault=${OBSIDIAN_VAULT_DIR:-/vault} config=${METACLAW_CONFIG_DIR:-/config} provider=${METACLAW_LLM_PROVIDER:-none} model=${METACLAW_LLM_MODEL:-none}" | tee -a "$LOG_PATH"
ls -la "${OBSIDIAN_VAULT_DIR:-/vault}" | head -40 | tee -a "$LOG_PATH"
ls -la "${METACLAW_CONFIG_DIR:-/config}" | head -40 | tee -a "$LOG_PATH"
`
}

func collectWizardInteractiveOptions(base wizardOptions) (wizardOptions, error) {
	opts := base
	if strings.TrimSpace(opts.ProjectDir) == "" {
		opts.ProjectDir = "./metaclaw-obsidian-agent"
	}
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("MetaClaw Interactive Wizard")
	fmt.Println("Press Enter to accept defaults.")

	var err error
	if opts.ProjectDir, err = promptString(reader, "Project folder", opts.ProjectDir); err != nil {
		return wizardOptions{}, err
	}
	opts.OutputPath = ""
	opts.VaultPath = ""
	opts.ConfigPath = ""
	opts.LogsPath = ""

	if opts.AgentName, err = promptString(reader, "Agent name", opts.AgentName); err != nil {
		return wizardOptions{}, err
	}
	lifecycleRaw, err := promptChoice(reader, "Lifecycle", []string{"ephemeral", "daemon", "debug"}, string(opts.Lifecycle))
	if err != nil {
		return wizardOptions{}, err
	}
	opts.Lifecycle = v1.LifecycleMode(lifecycleRaw)

	networkRaw, err := promptChoice(reader, "Network mode", []string{"none", "outbound", "all"}, opts.NetworkMode)
	if err != nil {
		return wizardOptions{}, err
	}
	opts.NetworkMode = networkRaw

	if opts.ReadOnlyVault, err = promptBool(reader, "Mount vault read-only", opts.ReadOnlyVault); err != nil {
		return wizardOptions{}, err
	}

	runtimeChoice, err := promptChoice(reader, "Runtime target", []string{"auto", "podman", "apple_container", "docker"}, "auto")
	if err != nil {
		return wizardOptions{}, err
	}
	if runtimeChoice == "auto" {
		opts.RuntimeTarget = ""
	} else {
		opts.RuntimeTarget = v1.RuntimeTarget(runtimeChoice)
	}

	if opts.LLMEnabled, err = promptBool(reader, "Enable LLM contract", opts.LLMEnabled); err != nil {
		return wizardOptions{}, err
	}
	if opts.LLMEnabled {
		providerRaw, err := promptChoice(reader, "LLM provider", []string{"gemini_openai", "openai_compatible"}, string(opts.LLMProvider))
		if err != nil {
			return wizardOptions{}, err
		}
		opts.LLMProvider = v1.LLMProvider(providerRaw)
		if opts.LLMModel, err = promptString(reader, "LLM model", opts.LLMModel); err != nil {
			return wizardOptions{}, err
		}
		baseURLDefault := strings.TrimSpace(opts.LLMBaseURL)
		if opts.LLMProvider == v1.LLMProviderOpenAICompatible && baseURLDefault == "" {
			baseURLDefault = "https://api.openai.com/v1"
		}
		if opts.LLMBaseURL, err = promptString(reader, "LLM base URL", baseURLDefault); err != nil {
			return wizardOptions{}, err
		}
		apiEnvDefault := strings.TrimSpace(opts.LLMAPIKeyEnv)
		if opts.LLMProvider == v1.LLMProviderOpenAICompatible && apiEnvDefault == "" {
			apiEnvDefault = "OPENAI_API_KEY"
		}
		if opts.LLMAPIKeyEnv, err = promptString(reader, "API key env var", apiEnvDefault); err != nil {
			return wizardOptions{}, err
		}
		opts.LLMFlagDisabled = false
	} else {
		opts.LLMProvider = ""
		opts.LLMModel = ""
		opts.LLMBaseURL = ""
		opts.LLMAPIKeyEnv = ""
		opts.LLMFlagDisabled = true
	}
	return opts, nil
}

func promptString(reader *bufio.Reader, label string, defaultValue string) (string, error) {
	for {
		if defaultValue == "" {
			fmt.Printf("%s: ", label)
		} else {
			fmt.Printf("%s [%s]: ", label, defaultValue)
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				line = strings.TrimSpace(line)
				if line == "" {
					if defaultValue == "" {
						return "", fmt.Errorf("input closed before %s was provided", strings.ToLower(label))
					}
					return defaultValue, nil
				}
				return line, nil
			}
			return "", err
		}
		value := strings.TrimSpace(line)
		if value != "" {
			return value, nil
		}
		if defaultValue != "" {
			return defaultValue, nil
		}
		fmt.Println("Value is required.")
	}
}

func promptChoice(reader *bufio.Reader, label string, choices []string, defaultValue string) (string, error) {
	choiceSet := make(map[string]struct{}, len(choices))
	for _, c := range choices {
		choiceSet[c] = struct{}{}
	}
	for {
		value, err := promptString(reader, fmt.Sprintf("%s (%s)", label, strings.Join(choices, "/")), defaultValue)
		if err != nil {
			return "", err
		}
		if _, ok := choiceSet[value]; ok {
			return value, nil
		}
		fmt.Printf("Invalid choice %q. Expected one of: %s\n", value, strings.Join(choices, ", "))
	}
}

func promptBool(reader *bufio.Reader, label string, defaultValue bool) (bool, error) {
	defaultToken := "no"
	if defaultValue {
		defaultToken = "yes"
	}
	for {
		value, err := promptChoice(reader, label, []string{"yes", "no"}, defaultToken)
		if err != nil {
			return false, err
		}
		return value == "yes", nil
	}
}

func hasFlagToken(args []string, names ...string) bool {
	for _, token := range args {
		for _, name := range names {
			if token == name {
				return true
			}
			if strings.HasPrefix(token, name+"=") {
				return true
			}
		}
	}
	return false
}

func renderWizardClawfile(cfg v1.Clawfile) ([]byte, error) {
	body, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	header := "# Generated by metaclaw wizard (Obsidian + LLM scaffold)\n" +
		"# Secrets are injected at runtime; API keys are not stored in this file.\n"
	return append([]byte(header), body...), nil
}
