package policy

import (
	"fmt"
	"sort"

	v1 "github.com/gmmakoele-ship-it/rafikiclaw/internal/claw/schema/v1"
	"github.com/gmmakoele-ship-it/rafikiclaw/internal/llm"
)

type Policy struct {
	Version      string        `json:"version"`
	Network      NetworkPolicy `json:"network"`
	Mounts       []MountPolicy `json:"mounts"`
	EnvAllowlist []string      `json:"envAllowlist"`
	Workdir      string        `json:"workdir,omitempty"`
	User         string        `json:"user,omitempty"`
}

type NetworkPolicy struct {
	Mode    string `json:"mode"`
	Allowed bool   `json:"allowed"`
}

type MountPolicy struct {
	Source   string `json:"source"`
	Target   string `json:"target"`
	ReadOnly bool   `json:"readOnly"`
}

func Compile(cfg v1.Clawfile) (Policy, error) {
	p := Policy{Version: "metaclaw.policy/v1"}

	mode := cfg.Agent.Habitat.Network.Mode
	switch mode {
	case "none":
		p.Network = NetworkPolicy{Mode: "none", Allowed: false}
	case "outbound", "all":
		p.Network = NetworkPolicy{Mode: mode, Allowed: true}
	default:
		return Policy{}, fmt.Errorf("unsupported network mode: %s", mode)
	}

	for _, m := range cfg.Agent.Habitat.Mounts {
		p.Mounts = append(p.Mounts, MountPolicy{
			Source:   m.Source,
			Target:   m.Target,
			ReadOnly: m.ReadOnly,
		})
	}
	sort.Slice(p.Mounts, func(i, j int) bool {
		if p.Mounts[i].Source == p.Mounts[j].Source {
			return p.Mounts[i].Target < p.Mounts[j].Target
		}
		return p.Mounts[i].Source < p.Mounts[j].Source
	})

	envSet := make(map[string]struct{})
	for k := range cfg.Agent.Habitat.Env {
		envSet[k] = struct{}{}
	}
	for _, k := range llm.AllowedEnvKeys(cfg.Agent.LLM) {
		envSet[k] = struct{}{}
	}
	for k := range envSet {
		p.EnvAllowlist = append(p.EnvAllowlist, k)
	}
	sort.Strings(p.EnvAllowlist)

	p.Workdir = cfg.Agent.Habitat.Workdir
	p.User = cfg.Agent.Habitat.User
	return p, nil
}
