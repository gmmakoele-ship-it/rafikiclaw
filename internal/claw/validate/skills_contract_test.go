package validate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	v1 "github.com/gmmakoele-ship-it/rafikiclaw/internal/claw/schema/v1"
)

func TestValidateSkillsWithCapabilityContract(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "run.sh"), []byte("#!/bin/sh\necho hi\n"), 0o644); err != nil {
		t.Fatalf("write skill file: %v", err)
	}
	contract := `apiVersion: metaclaw.capability/v1
kind: CapabilityContract
metadata:
  name: obsidian.reader
  version: v1.0.0
permissions:
  network: outbound
  mounts:
    - target: /vault
      access: ro
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

	cfg := v1.Clawfile{
		APIVersion: "rafikiclaw/v1",
		Kind:       "Agent",
		Agent: v1.AgentSpec{
			Name:    "a",
			Species: v1.SpeciesMicro,
			Habitat: v1.HabitatSpec{
				Network: v1.NetworkSpec{Mode: "outbound"},
				Mounts: []v1.MountSpec{
					{Source: filepath.Join(root, "vault"), Target: "/vault", ReadOnly: true},
				},
				Env: map[string]string{
					"OBSIDIAN_VAULT_DIR": "/vault",
				},
			},
			LLM: v1.LLMSpec{
				Provider: v1.LLMProviderOpenAICompatible,
				Model:    "gpt-4.1-mini",
			},
			Runtime: v1.RuntimeSpec{Target: v1.RuntimeDocker},
			Skills: []v1.SkillRef{
				{Path: "skill", Version: "v1.0.0"},
			},
		},
	}
	if _, err := NormalizeAndValidate(cfg, filepath.Join(root, "agent.claw")); err != nil {
		t.Fatalf("NormalizeAndValidate() error = %v", err)
	}
}

func TestValidateSkillsRejectsContractPolicyMismatch(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "run.sh"), []byte("#!/bin/sh\necho hi\n"), 0o644); err != nil {
		t.Fatalf("write skill file: %v", err)
	}
	contract := `apiVersion: metaclaw.capability/v1
kind: CapabilityContract
metadata:
  name: obsidian.writer
  version: v1.0.0
permissions:
  network: outbound
  mounts:
    - target: /vault
      access: rw
      required: true
`
	if err := os.WriteFile(filepath.Join(skillDir, "capability.contract.yaml"), []byte(contract), 0o644); err != nil {
		t.Fatalf("write contract: %v", err)
	}

	cfg := v1.Clawfile{
		APIVersion: "rafikiclaw/v1",
		Kind:       "Agent",
		Agent: v1.AgentSpec{
			Name:    "a",
			Species: v1.SpeciesMicro,
			Habitat: v1.HabitatSpec{
				Network: v1.NetworkSpec{Mode: "none"},
				Mounts: []v1.MountSpec{
					{Source: filepath.Join(root, "vault"), Target: "/vault", ReadOnly: true},
				},
			},
			Skills: []v1.SkillRef{
				{Path: "skill", Version: "v1.0.0"},
			},
		},
	}
	_, err := NormalizeAndValidate(cfg, filepath.Join(root, "agent.claw"))
	if err == nil {
		t.Fatal("expected policy mismatch validation error")
	}
	if !strings.Contains(err.Error(), "requires network=outbound") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateSkillsByIDRequireVersionAndDigest(t *testing.T) {
	cfg := v1.Clawfile{
		APIVersion: "rafikiclaw/v1",
		Kind:       "Agent",
		Agent: v1.AgentSpec{
			Name:    "a",
			Species: v1.SpeciesNano,
			Skills: []v1.SkillRef{
				{ID: "metaclaw/obsidian-sync"},
			},
		},
	}
	_, err := NormalizeAndValidate(cfg, "agent.claw")
	if err == nil {
		t.Fatal("expected skill id reproducibility validation error")
	}
	if !strings.Contains(err.Error(), "requires version") {
		t.Fatalf("unexpected error: %v", err)
	}
}
