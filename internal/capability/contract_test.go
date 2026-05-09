package capability

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	v1 "github.com/gmmakoele-ship-it/rafikiclaw/internal/claw/schema/v1"
)

func TestLoadFromSkillPathAndValidateAgainstAgent(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "handler.sh"), []byte("#!/bin/sh\necho ok\n"), 0o644); err != nil {
		t.Fatalf("write skill file: %v", err)
	}
	contract := `apiVersion: rafikiclaw.capability/v1
kind: CapabilityContract
metadata:
  name: obsidian.ingest
  version: v1.0.0
permissions:
  network: outbound
  mounts:
    - target: /vault
      access: rw
      required: true
  env:
    - OBSIDIAN_VAULT_DIR
  secrets:
    - OPENAI_API_KEY
compatibility:
  runtimeTargets: [docker, podman]
`
	if err := os.WriteFile(filepath.Join(skillDir, "capability.contract.yaml"), []byte(contract), 0o644); err != nil {
		t.Fatalf("write contract: %v", err)
	}

	c, path, err := LoadFromSkillPath(skillDir)
	if err != nil {
		t.Fatalf("LoadFromSkillPath() error = %v", err)
	}
	if !strings.HasSuffix(path, "capability.contract.yaml") {
		t.Fatalf("unexpected contract path: %s", path)
	}

	agent := v1.AgentSpec{
		Name:    "a",
		Species: v1.SpeciesMicro,
		Habitat: v1.HabitatSpec{
			Network: v1.NetworkSpec{Mode: "outbound"},
			Mounts: []v1.MountSpec{
				{Source: "/tmp/vault", Target: "/vault"},
			},
			Env: map[string]string{
				"OBSIDIAN_VAULT_DIR": "/vault",
			},
		},
		LLM: v1.LLMSpec{
			Provider:  v1.LLMProviderOpenAICompatible,
			Model:     "gpt-4.1-mini",
			APIKeyEnv: "OPENAI_API_KEY",
		},
		Runtime: v1.RuntimeSpec{Target: v1.RuntimeDocker},
	}
	if err := ValidateAgainstAgent(c, agent); err != nil {
		t.Fatalf("ValidateAgainstAgent() error = %v", err)
	}
}

func TestValidateAgainstAgentRejectsMissingRequiredMount(t *testing.T) {
	c := Contract{
		APIVersion: ContractAPIVersion,
		Kind:       ContractKind,
		Metadata:   Metadata{Name: "x", Version: "v1"},
		Permissions: Permissions{
			Network: "none",
			Mounts: []MountPermission{
				{Target: "/vault", Access: "ro", Required: true},
			},
		},
	}
	if err := Validate(c); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	agent := v1.AgentSpec{
		Name:    "a",
		Species: v1.SpeciesMicro,
		Habitat: v1.HabitatSpec{
			Network: v1.NetworkSpec{Mode: "none"},
			Mounts:  []v1.MountSpec{},
		},
	}
	err := ValidateAgainstAgent(c, agent)
	if err == nil {
		t.Fatal("expected missing mount validation error")
	}
	if !strings.Contains(err.Error(), "requires mount target /vault") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateAgainstAgentRejectsRuntimeAutoWhenContractPinsTargets(t *testing.T) {
	c := Contract{
		APIVersion: ContractAPIVersion,
		Kind:       ContractKind,
		Metadata:   Metadata{Name: "x", Version: "v1"},
		Permissions: Permissions{
			Network: "none",
		},
		Compatibility: Compatibility{
			RuntimeTargets: []string{"docker", "podman"},
		},
	}
	if err := Validate(c); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	agent := v1.AgentSpec{
		Name:    "a",
		Species: v1.SpeciesMicro,
		Habitat: v1.HabitatSpec{
			Network: v1.NetworkSpec{Mode: "none"},
		},
		// runtime.target intentionally omitted (auto mode)
	}
	err := ValidateAgainstAgent(c, agent)
	if err == nil {
		t.Fatal("expected runtime.target requirement error")
	}
	if !strings.Contains(err.Error(), "set agent.runtime.target explicitly") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadFromSkillPathRequiresContract(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "handler.sh"), []byte("echo hi\n"), 0o644); err != nil {
		t.Fatalf("write skill file: %v", err)
	}
	_, _, err := LoadFromSkillPath(skillDir)
	if err == nil {
		t.Fatal("expected missing contract error")
	}
	if !strings.Contains(err.Error(), "missing capability contract") {
		t.Fatalf("unexpected error: %v", err)
	}
}
