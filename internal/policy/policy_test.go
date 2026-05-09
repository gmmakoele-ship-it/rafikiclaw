package policy

import (
	"testing"

	v1 "github.com/gmmakoele-ship-it/rafikiclaw/internal/claw/schema/v1"
)

func TestCompileDenyByDefaultNetwork(t *testing.T) {
	cfg := v1.Clawfile{
		APIVersion: "rafikiclaw/v1",
		Kind:       "Agent",
		Agent: v1.AgentSpec{
			Name:      "a",
			Species:   v1.SpeciesNano,
			Lifecycle: v1.LifecycleEphemeral,
			Habitat: v1.HabitatSpec{
				Network: v1.NetworkSpec{Mode: "none"},
			},
		},
	}
	p, err := Compile(cfg)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	if p.Network.Allowed {
		t.Fatal("expected network to be denied for mode=none")
	}
}

func TestCompileIncludesLLMEnvAllowlist(t *testing.T) {
	cfg := v1.Clawfile{
		APIVersion: "rafikiclaw/v1",
		Kind:       "Agent",
		Agent: v1.AgentSpec{
			Name:      "a",
			Species:   v1.SpeciesNano,
			Lifecycle: v1.LifecycleEphemeral,
			Habitat: v1.HabitatSpec{
				Network: v1.NetworkSpec{Mode: "none"},
			},
			LLM: v1.LLMSpec{
				Provider:  v1.LLMProviderGeminiOpenAI,
				Model:     "gemini-2.5-pro",
				BaseURL:   "https://generativelanguage.googleapis.com/v1beta/openai/",
				APIKeyEnv: "GEMINI_API_KEY",
			},
		},
	}
	p, err := Compile(cfg)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	assertContains(t, p.EnvAllowlist, "GEMINI_API_KEY")
	assertContains(t, p.EnvAllowlist, "OPENAI_API_KEY")
	assertContains(t, p.EnvAllowlist, "OPENAI_BASE_URL")
}

func assertContains(t *testing.T, list []string, want string) {
	t.Helper()
	for _, v := range list {
		if v == want {
			return
		}
	}
	t.Fatalf("expected %q in %v", want, list)
}
