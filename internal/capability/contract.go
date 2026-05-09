package capability

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	v1 "github.com/gmmakoele-ship-it/rafikiclaw/internal/claw/schema/v1"
	"github.com/gmmakoele-ship-it/rafikiclaw/internal/llm"
	"gopkg.in/yaml.v3"
)

const (
	ContractAPIVersion = "rafikiclaw.capability/v1"
	ContractKind       = "CapabilityContract"
)

var envNameRef = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type Contract struct {
	APIVersion    string        `yaml:"apiVersion" json:"apiVersion"`
	Kind          string        `yaml:"kind" json:"kind"`
	Metadata      Metadata      `yaml:"metadata" json:"metadata"`
	Interface     IOInterface   `yaml:"interface,omitempty" json:"interface,omitempty"`
	Permissions   Permissions   `yaml:"permissions" json:"permissions"`
	SideEffects   SideEffects   `yaml:"sideEffects,omitempty" json:"sideEffects,omitempty"`
	Compatibility Compatibility `yaml:"compatibility,omitempty" json:"compatibility,omitempty"`
	Observability Observability `yaml:"observability,omitempty" json:"observability,omitempty"`
}

type Metadata struct {
	Name        string `yaml:"name" json:"name"`
	Version     string `yaml:"version" json:"version"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
}

type IOInterface struct {
	Inputs  []IOField `yaml:"inputs,omitempty" json:"inputs,omitempty"`
	Outputs []IOField `yaml:"outputs,omitempty" json:"outputs,omitempty"`
}

type IOField struct {
	Name        string `yaml:"name" json:"name"`
	Type        string `yaml:"type" json:"type"`
	Required    bool   `yaml:"required,omitempty" json:"required,omitempty"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
}

type Permissions struct {
	Network string            `yaml:"network,omitempty" json:"network,omitempty"`
	Mounts  []MountPermission `yaml:"mounts,omitempty" json:"mounts,omitempty"`
	Env     []string          `yaml:"env,omitempty" json:"env,omitempty"`
	Secrets []string          `yaml:"secrets,omitempty" json:"secrets,omitempty"`
}

type MountPermission struct {
	Target      string `yaml:"target" json:"target"`
	Access      string `yaml:"access" json:"access"` // ro|rw
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	Required    bool   `yaml:"required,omitempty" json:"required,omitempty"`
}

type SideEffects struct {
	Writes       []string `yaml:"writes,omitempty" json:"writes,omitempty"`
	ExternalAPIs []string `yaml:"externalAPIs,omitempty" json:"externalAPIs,omitempty"`
}

type Compatibility struct {
	MinMetaclawVersion string   `yaml:"minMetaclawVersion,omitempty" json:"minMetaclawVersion,omitempty"`
	RuntimeTargets     []string `yaml:"runtimeTargets,omitempty" json:"runtimeTargets,omitempty"`
}

type Observability struct {
	RequiredEvents []string `yaml:"requiredEvents,omitempty" json:"requiredEvents,omitempty"`
	LogFields      []string `yaml:"logFields,omitempty" json:"logFields,omitempty"`
}

func DiscoverContractPath(skillPath string) (string, bool, error) {
	st, err := os.Stat(skillPath)
	if err != nil {
		return "", false, err
	}
	baseDir := skillPath
	if !st.IsDir() {
		baseDir = filepath.Dir(skillPath)
	}
	candidates := []string{
		filepath.Join(baseDir, "capability.contract.yaml"),
		filepath.Join(baseDir, "capability.contract.yml"),
		filepath.Join(baseDir, "capability.contract.json"),
	}
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && !info.IsDir() {
			return c, true, nil
		}
	}
	return "", false, nil
}

func LoadFromSkillPath(skillPath string) (Contract, string, error) {
	contractPath, ok, err := DiscoverContractPath(skillPath)
	if err != nil {
		return Contract{}, "", fmt.Errorf("discover capability contract: %w", err)
	}
	if !ok {
		return Contract{}, "", fmt.Errorf("missing capability contract (expected capability.contract.yaml|yml|json)")
	}
	b, err := os.ReadFile(contractPath)
	if err != nil {
		return Contract{}, "", fmt.Errorf("read capability contract: %w", err)
	}
	var c Contract
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil {
		return Contract{}, "", fmt.Errorf("parse capability contract (%s): %w", filepath.Base(contractPath), err)
	}
	if err := Validate(c); err != nil {
		return Contract{}, "", err
	}
	return c, contractPath, nil
}

func Validate(c Contract) error {
	if c.APIVersion != ContractAPIVersion {
		return fmt.Errorf("capability contract apiVersion must be %s", ContractAPIVersion)
	}
	if c.Kind != ContractKind {
		return fmt.Errorf("capability contract kind must be %s", ContractKind)
	}
	c.Metadata.Name = strings.TrimSpace(c.Metadata.Name)
	c.Metadata.Version = strings.TrimSpace(c.Metadata.Version)
	if c.Metadata.Name == "" {
		return fmt.Errorf("capability contract metadata.name is required")
	}
	if c.Metadata.Version == "" {
		return fmt.Errorf("capability contract metadata.version is required")
	}

	network := strings.TrimSpace(c.Permissions.Network)
	if network == "" {
		network = "none"
	}
	switch network {
	case "none", "outbound", "all":
	default:
		return fmt.Errorf("capability contract permissions.network must be one of none,outbound,all")
	}

	for _, m := range c.Permissions.Mounts {
		target := strings.TrimSpace(m.Target)
		if target == "" || !strings.HasPrefix(target, "/") {
			return fmt.Errorf("capability contract mount target must be absolute (got %q)", m.Target)
		}
		switch strings.TrimSpace(m.Access) {
		case "ro", "rw":
		default:
			return fmt.Errorf("capability contract mount access must be ro|rw (target=%s)", m.Target)
		}
	}

	if err := validateEnvNames(c.Permissions.Env, "permissions.env"); err != nil {
		return err
	}
	if err := validateEnvNames(c.Permissions.Secrets, "permissions.secrets"); err != nil {
		return err
	}

	if err := validateIOFields(c.Interface.Inputs, "interface.inputs"); err != nil {
		return err
	}
	if err := validateIOFields(c.Interface.Outputs, "interface.outputs"); err != nil {
		return err
	}

	for _, rt := range c.Compatibility.RuntimeTargets {
		rt = strings.TrimSpace(rt)
		if rt == "" {
			return fmt.Errorf("capability contract compatibility.runtimeTargets must not contain empty values")
		}
		if !v1.RuntimeTarget(rt).Valid() {
			return fmt.Errorf("capability contract compatibility.runtimeTargets contains unsupported runtime %q", rt)
		}
	}

	return nil
}

func ValidateAgainstAgent(c Contract, agent v1.AgentSpec) error {
	reqNetwork := strings.TrimSpace(c.Permissions.Network)
	if reqNetwork == "" {
		reqNetwork = "none"
	}
	agentNetwork := strings.TrimSpace(agent.Habitat.Network.Mode)
	if agentNetwork == "" {
		agentNetwork = "none"
	}
	if networkRank(reqNetwork) > networkRank(agentNetwork) {
		return fmt.Errorf("skill requires network=%s but agent habitat grants network=%s", reqNetwork, agentNetwork)
	}

	mountByTarget := make(map[string]v1.MountSpec, len(agent.Habitat.Mounts))
	for _, m := range agent.Habitat.Mounts {
		mountByTarget[m.Target] = m
	}
	for _, req := range c.Permissions.Mounts {
		agentMount, ok := mountByTarget[req.Target]
		if !ok {
			if req.Required {
				return fmt.Errorf("skill requires mount target %s but it is not present in habitat.mounts", req.Target)
			}
			continue
		}
		if req.Access == "rw" && agentMount.ReadOnly {
			return fmt.Errorf("skill requires rw mount at %s but habitat mount is read-only", req.Target)
		}
	}

	availableEnv := make(map[string]struct{})
	for k := range agent.Habitat.Env {
		availableEnv[k] = struct{}{}
	}
	for _, k := range llm.AllowedEnvKeys(agent.LLM) {
		availableEnv[k] = struct{}{}
	}
	for _, envKey := range c.Permissions.Env {
		if _, ok := availableEnv[envKey]; !ok {
			return fmt.Errorf("skill requires env %s but agent does not declare it in habitat.env/llm contract", envKey)
		}
	}
	for _, secretKey := range c.Permissions.Secrets {
		if _, ok := availableEnv[secretKey]; !ok {
			return fmt.Errorf("skill requires secret %s but agent does not declare a binding for it", secretKey)
		}
	}

	if len(c.Compatibility.RuntimeTargets) > 0 {
		if agent.Runtime.Target == "" {
			return fmt.Errorf("skill declares compatibility.runtimeTargets=%s; set agent.runtime.target explicitly", strings.Join(c.Compatibility.RuntimeTargets, ","))
		}
		allowed := make(map[string]struct{}, len(c.Compatibility.RuntimeTargets))
		for _, rt := range c.Compatibility.RuntimeTargets {
			allowed[strings.TrimSpace(rt)] = struct{}{}
		}
		if _, ok := allowed[string(agent.Runtime.Target)]; !ok {
			return fmt.Errorf("skill supports runtimes %s but agent runtime.target=%s", strings.Join(c.Compatibility.RuntimeTargets, ","), agent.Runtime.Target)
		}
	}
	return nil
}

func validateIOFields(fields []IOField, section string) error {
	seen := make(map[string]struct{}, len(fields))
	for i, f := range fields {
		name := strings.TrimSpace(f.Name)
		typ := strings.TrimSpace(f.Type)
		if name == "" {
			return fmt.Errorf("capability contract %s[%d].name is required", section, i)
		}
		if typ == "" {
			return fmt.Errorf("capability contract %s[%d].type is required", section, i)
		}
		if _, exists := seen[name]; exists {
			return fmt.Errorf("capability contract %s contains duplicate field name %q", section, name)
		}
		seen[name] = struct{}{}
	}
	return nil
}

func validateEnvNames(values []string, section string) error {
	for _, raw := range values {
		v := strings.TrimSpace(raw)
		if !envNameRef.MatchString(v) {
			return fmt.Errorf("capability contract %s contains invalid env name %q", section, raw)
		}
	}
	return nil
}

func networkRank(mode string) int {
	switch mode {
	case "none":
		return 0
	case "outbound":
		return 1
	case "all":
		return 2
	default:
		return -1
	}
}

func RequiredContractFileNames() []string {
	out := []string{
		"capability.contract.yaml",
		"capability.contract.yml",
		"capability.contract.json",
	}
	sort.Strings(out)
	return out
}
