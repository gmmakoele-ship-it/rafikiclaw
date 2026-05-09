package validate

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/gmmakoele-ship-it/rafikiclaw/internal/capability"
	v1 "github.com/gmmakoele-ship-it/rafikiclaw/internal/claw/schema/v1"
)

var digestRef = regexp.MustCompile(`.+@sha256:[a-fA-F0-9]{64}$`)
var envNameRef = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func NormalizeAndValidate(cfg v1.Clawfile, clawfilePath string) (v1.Clawfile, error) {
	if err := cfg.ValidateBasics(); err != nil {
		return v1.Clawfile{}, err
	}

	if cfg.Agent.Lifecycle == "" {
		cfg.Agent.Lifecycle = v1.LifecycleEphemeral
	}
	if cfg.Agent.Habitat.Network.Mode == "" {
		cfg.Agent.Habitat.Network.Mode = "none"
	}

	profile, ok := v1.SpeciesProfileFor(cfg.Agent.Species)
	if !ok {
		return v1.Clawfile{}, fmt.Errorf("unknown species: %s", cfg.Agent.Species)
	}
	if cfg.Agent.Runtime.Image == "" {
		cfg.Agent.Runtime.Image = profile.DefaultImage
	}
	if cfg.Agent.Runtime.Resources.CPU == "" {
		cfg.Agent.Runtime.Resources.CPU = profile.DefaultCPU
	}
	if cfg.Agent.Runtime.Resources.Memory == "" {
		cfg.Agent.Runtime.Resources.Memory = profile.DefaultMem
	}
	if len(cfg.Agent.Command) == 0 {
		cfg.Agent.Command = []string{"sh", "-lc", "echo MetaClaw agent started"}
	}
	if err := normalizeLLM(&cfg.Agent.LLM); err != nil {
		return v1.Clawfile{}, err
	}

	if !digestRef.MatchString(cfg.Agent.Runtime.Image) {
		return v1.Clawfile{}, fmt.Errorf("agent.runtime.image must be digest-pinned (example: image@sha256:...)")
	}

	if err := validateNetwork(cfg.Agent.Habitat.Network.Mode); err != nil {
		return v1.Clawfile{}, err
	}
	if err := validateMounts(cfg.Agent.Habitat.Mounts); err != nil {
		return v1.Clawfile{}, err
	}
	if err := validateSkills(cfg, filepath.Dir(clawfilePath)); err != nil {
		return v1.Clawfile{}, err
	}

	cfg.Agent.Habitat.Env = sortedMap(cfg.Agent.Habitat.Env)
	return cfg, nil
}

func normalizeLLM(spec *v1.LLMSpec) error {
	if spec == nil {
		return nil
	}
	hasProvider := spec.Provider != ""
	hasOther := strings.TrimSpace(spec.Model) != "" || strings.TrimSpace(spec.BaseURL) != "" || strings.TrimSpace(spec.APIKeyEnv) != ""
	if !hasProvider {
		if hasOther {
			return fmt.Errorf("agent.llm.provider is required when llm fields are set")
		}
		return nil
	}

	spec.Model = strings.TrimSpace(spec.Model)
	spec.BaseURL = strings.TrimSpace(spec.BaseURL)
	spec.APIKeyEnv = strings.TrimSpace(spec.APIKeyEnv)

	if spec.Model == "" {
		return fmt.Errorf("agent.llm.model is required when agent.llm.provider is set")
	}
	switch spec.Provider {
	case v1.LLMProviderGeminiOpenAI:
		if spec.BaseURL == "" {
			spec.BaseURL = "https://generativelanguage.googleapis.com/v1beta/openai/"
		}
		if spec.APIKeyEnv == "" {
			spec.APIKeyEnv = "GEMINI_API_KEY"
		}
	case v1.LLMProviderOpenAICompatible:
		if spec.APIKeyEnv == "" {
			spec.APIKeyEnv = "OPENAI_API_KEY"
		}
	}
	if !envNameRef.MatchString(spec.APIKeyEnv) {
		return fmt.Errorf("agent.llm.apiKeyEnv must be a valid environment variable name")
	}
	return nil
}

func validateNetwork(mode string) error {
	switch mode {
	case "none", "outbound", "all":
		return nil
	default:
		return fmt.Errorf("agent.habitat.network.mode must be one of none,outbound,all")
	}
}

func validateMounts(mounts []v1.MountSpec) error {
	seenTargets := make(map[string]struct{}, len(mounts))
	for _, m := range mounts {
		source := strings.TrimSpace(m.Source)
		target := strings.TrimSpace(m.Target)
		if source == "" || target == "" {
			return fmt.Errorf("every habitat mount requires source and target")
		}
		if !filepath.IsAbs(source) {
			return fmt.Errorf("habitat mount source must be an absolute path (got %q)", m.Source)
		}
		cleanSource := filepath.Clean(source)
		if cleanSource != source {
			return fmt.Errorf("habitat mount source must be normalized (got %q; want %q)", m.Source, cleanSource)
		}
		if !path.IsAbs(target) {
			return fmt.Errorf("habitat mount target must be an absolute container path (got %q)", m.Target)
		}
		cleanTarget := path.Clean(target)
		if cleanTarget == "/" {
			return fmt.Errorf("habitat mount target cannot be root /")
		}
		if cleanTarget != target {
			return fmt.Errorf("habitat mount target must be normalized (got %q; want %q)", m.Target, cleanTarget)
		}
		if _, ok := seenTargets[target]; ok {
			return fmt.Errorf("duplicate habitat mount target: %s", target)
		}
		seenTargets[target] = struct{}{}
	}
	return nil
}

func validateSkills(cfg v1.Clawfile, baseDir string) error {
	for _, s := range cfg.Agent.Skills {
		hasPath := s.Path != ""
		hasID := s.ID != ""
		if hasPath == hasID {
			return fmt.Errorf("skill entries must specify exactly one of path or id")
		}
		if hasPath {
			resolved := s.Path
			if !filepath.IsAbs(resolved) {
				resolved = filepath.Join(baseDir, s.Path)
			}
			if _, err := os.Stat(resolved); err != nil {
				return fmt.Errorf("skill path not found: %s", s.Path)
			}
			contract, contractPath, err := capability.LoadFromSkillPath(resolved)
			if err != nil {
				return fmt.Errorf("skill %s: %w", s.Path, err)
			}
			if strings.TrimSpace(s.Version) != "" && strings.TrimSpace(s.Version) != strings.TrimSpace(contract.Metadata.Version) {
				return fmt.Errorf("skill %s: version mismatch between clawfile (%s) and contract (%s)", s.Path, s.Version, contract.Metadata.Version)
			}
			if err := capability.ValidateAgainstAgent(contract, cfg.Agent); err != nil {
				return fmt.Errorf("skill %s contract (%s): %w", s.Path, filepath.Base(contractPath), err)
			}
			continue
		}
		if strings.TrimSpace(s.Version) == "" {
			return fmt.Errorf("skill id %s requires version for reproducible resolution", s.ID)
		}
		if strings.TrimSpace(s.Digest) == "" {
			return fmt.Errorf("skill id %s requires digest for reproducible resolution", s.ID)
		}
	}
	return nil
}

func sortedMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	keys := make([]string, 0, len(in))
	for k := range in {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make(map[string]string, len(in))
	for _, k := range keys {
		out[k] = in[k]
	}
	return out
}
