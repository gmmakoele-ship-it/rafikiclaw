package v1

import "fmt"

type Species string

type LifecycleMode string

type RuntimeTarget string
type LLMProvider string

const (
	SpeciesNano  Species = "nano"
	SpeciesMicro Species = "micro"
	SpeciesMega  Species = "mega"
)

const (
	LifecycleEphemeral LifecycleMode = "ephemeral"
	LifecycleDaemon    LifecycleMode = "daemon"
	LifecycleDebug     LifecycleMode = "debug"
)

const (
	RuntimePodman RuntimeTarget = "podman"
	RuntimeApple  RuntimeTarget = "apple_container"
	RuntimeDocker RuntimeTarget = "docker"
)

const (
	LLMProviderOpenAICompatible LLMProvider = "openai_compatible"
	LLMProviderGeminiOpenAI     LLMProvider = "gemini_openai"
	LLMProviderAnthropic        LLMProvider = "anthropic"
)

type Clawfile struct {
	APIVersion string    `yaml:"apiVersion" json:"apiVersion"`
	Kind       string    `yaml:"kind" json:"kind"`
	Agent      AgentSpec `yaml:"agent" json:"agent"`
}

type AgentSpec struct {
	Name      string        `yaml:"name" json:"name"`
	Species   Species       `yaml:"species" json:"species"`
	Lifecycle LifecycleMode `yaml:"lifecycle,omitempty" json:"lifecycle,omitempty"`
	Habitat   HabitatSpec   `yaml:"habitat,omitempty" json:"habitat,omitempty"`
	LLM       LLMSpec       `yaml:"llm,omitempty" json:"llm,omitempty"`
	Soul      SoulSpec      `yaml:"soul,omitempty" json:"soul,omitempty"`
	Skills    []SkillRef    `yaml:"skills,omitempty" json:"skills,omitempty"`
	Runtime   RuntimeSpec   `yaml:"runtime,omitempty" json:"runtime,omitempty"`
	Command   []string      `yaml:"command,omitempty" json:"command,omitempty"`
}

type HabitatSpec struct {
	Network NetworkSpec       `yaml:"network,omitempty" json:"network,omitempty"`
	Mounts  []MountSpec       `yaml:"mounts,omitempty" json:"mounts,omitempty"`
	Env     map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
	Workdir string            `yaml:"workdir,omitempty" json:"workdir,omitempty"`
	User    string            `yaml:"user,omitempty" json:"user,omitempty"`
}

type NetworkSpec struct {
	Mode string `yaml:"mode,omitempty" json:"mode,omitempty"`
}

type MountSpec struct {
	Source   string `yaml:"source" json:"source"`
	Target   string `yaml:"target" json:"target"`
	ReadOnly bool   `yaml:"readOnly,omitempty" json:"readOnly,omitempty"`
}

type SoulSpec struct {
	Persona string `yaml:"persona,omitempty" json:"persona,omitempty"`
	Memory  string `yaml:"memory,omitempty" json:"memory,omitempty"`
}

type SkillRef struct {
	Path    string `yaml:"path,omitempty" json:"path,omitempty"`
	ID      string `yaml:"id,omitempty" json:"id,omitempty"`
	Version string `yaml:"version,omitempty" json:"version,omitempty"`
	Digest  string `yaml:"digest,omitempty" json:"digest,omitempty"`
}

type RuntimeSpec struct {
	Target    RuntimeTarget `yaml:"target,omitempty" json:"target,omitempty"`
	Image     string        `yaml:"image,omitempty" json:"image,omitempty"`
	Resources ResourceSpec  `yaml:"resources,omitempty" json:"resources,omitempty"`
}

type LLMSpec struct {
	Provider  LLMProvider `yaml:"provider,omitempty" json:"provider,omitempty"`
	Model     string      `yaml:"model,omitempty" json:"model,omitempty"`
	BaseURL   string      `yaml:"baseURL,omitempty" json:"baseURL,omitempty"`
	APIKeyEnv string      `yaml:"apiKeyEnv,omitempty" json:"apiKeyEnv,omitempty"`
}

type ResourceSpec struct {
	CPU    string `yaml:"cpu,omitempty" json:"cpu,omitempty"`
	Memory string `yaml:"memory,omitempty" json:"memory,omitempty"`
}

type SpeciesProfile struct {
	Name         Species      `json:"name"`
	DefaultImage string       `json:"defaultImage"`
	DefaultCPU   string       `json:"defaultCPU"`
	DefaultMem   string       `json:"defaultMemory"`
	RuntimeHints []string     `json:"runtimeHints"`
	Allowed      AllowedPatch `json:"allowedOverrides"`
}

type AllowedPatch struct {
	AllowResourceOverride bool `json:"allowResourceOverride"`
	AllowImageOverride    bool `json:"allowImageOverride"`
}

func (s Species) Valid() bool {
	switch s {
	case SpeciesNano, SpeciesMicro, SpeciesMega:
		return true
	default:
		return false
	}
}

func (l LifecycleMode) Valid() bool {
	switch l {
	case LifecycleEphemeral, LifecycleDaemon, LifecycleDebug:
		return true
	default:
		return false
	}
}

func (r RuntimeTarget) Valid() bool {
	switch r {
	case RuntimePodman, RuntimeApple, RuntimeDocker, "":
		return true
	default:
		return false
	}
}

func (p LLMProvider) Valid() bool {
	switch p {
	case "", LLMProviderOpenAICompatible, LLMProviderGeminiOpenAI, LLMProviderAnthropic:
		return true
	default:
		return false
	}
}

func (c Clawfile) ValidateBasics() error {
	if c.APIVersion != "rafikiclaw/v1" {
		return fmt.Errorf("apiVersion must be rafikiclaw/v1")
	}
	if c.Kind != "Agent" {
		return fmt.Errorf("kind must be Agent")
	}
	if c.Agent.Name == "" {
		return fmt.Errorf("agent.name is required")
	}
	if !c.Agent.Species.Valid() {
		return fmt.Errorf("agent.species must be one of nano,micro,mega")
	}
	if c.Agent.Lifecycle != "" && !c.Agent.Lifecycle.Valid() {
		return fmt.Errorf("agent.lifecycle must be one of ephemeral,daemon,debug")
	}
	if !c.Agent.Runtime.Target.Valid() {
		return fmt.Errorf("agent.runtime.target must be one of podman,apple_container,docker")
	}
	if !c.Agent.LLM.Provider.Valid() {
		return fmt.Errorf("agent.llm.provider must be one of openai_compatible,gemini_openai,anthropic")
	}
	return nil
}
