package llm

import (
	"testing"

	v1 "github.com/gmmakoele-ship-it/rafikiclaw/internal/claw/schema/v1"
)

func TestResolveDisabledWhenNoProvider(t *testing.T) {
	res, err := Resolve(v1.LLMSpec{}, RuntimeOptions{})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if res.Enabled {
		t.Fatal("expected disabled resolver when provider is empty")
	}
}

func TestResolveGeminiOpenAI(t *testing.T) {
	spec := v1.LLMSpec{
		Provider:  v1.LLMProviderGeminiOpenAI,
		Model:     "gemini-2.5-pro",
		BaseURL:   "https://generativelanguage.googleapis.com/v1beta/openai/",
		APIKeyEnv: "GEMINI_API_KEY",
	}
	res, err := Resolve(spec, RuntimeOptions{APIKey: "abc-123"})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if !res.Enabled {
		t.Fatal("expected enabled resolver")
	}
	if res.Env["GEMINI_API_KEY"] != "abc-123" {
		t.Fatalf("expected GEMINI_API_KEY to be populated")
	}
	if res.Env["OPENAI_API_KEY"] != "abc-123" {
		t.Fatalf("expected OPENAI_API_KEY mirror for openai-compatible SDK usage")
	}
	if res.Env["OPENAI_BASE_URL"] != spec.BaseURL {
		t.Fatalf("expected OPENAI_BASE_URL mirror, got %q", res.Env["OPENAI_BASE_URL"])
	}
}

func TestResolveMissingKey(t *testing.T) {
	spec := v1.LLMSpec{Provider: v1.LLMProviderOpenAICompatible, Model: "gpt-4.1", APIKeyEnv: "OPENAI_API_KEY"}
	_, err := Resolve(spec, RuntimeOptions{})
	if err == nil {
		t.Fatal("expected error when key is missing")
	}
}

func TestResolveAnthropic(t *testing.T) {
	spec := v1.LLMSpec{
		Provider:  v1.LLMProviderAnthropic,
		Model:     "claude-3-5-sonnet-latest",
		BaseURL:   "https://api.anthropic.com/v1",
		APIKeyEnv: "ANTHROPIC_API_KEY",
	}
	res, err := Resolve(spec, RuntimeOptions{APIKey: "abc-123"})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if !res.Enabled {
		t.Fatal("expected enabled resolver")
	}
	if res.Env["ANTHROPIC_API_KEY"] != "abc-123" {
		t.Fatalf("expected ANTHROPIC_API_KEY to be populated")
	}
	if res.Env["ANTHROPIC_BASE_URL"] != spec.BaseURL {
		t.Fatalf("expected ANTHROPIC_BASE_URL mirror, got %q", res.Env["ANTHROPIC_BASE_URL"])
	}
}

func TestAllowedEnvKeys(t *testing.T) {
	spec := v1.LLMSpec{
		Provider:  v1.LLMProviderGeminiOpenAI,
		Model:     "gemini-2.5-pro",
		BaseURL:   "https://generativelanguage.googleapis.com/v1beta/openai/",
		APIKeyEnv: "GEMINI_API_KEY",
	}
	keys := AllowedEnvKeys(spec)
	mustContain(t, keys, "GEMINI_API_KEY")
	mustContain(t, keys, "OPENAI_API_KEY")
	mustContain(t, keys, "OPENAI_BASE_URL")
	mustContain(t, keys, "METACLAW_LLM_MODEL")
}

func mustContain(t *testing.T, list []string, want string) {
	t.Helper()
	for _, v := range list {
		if v == want {
			return
		}
	}
	t.Fatalf("expected key %q in %v", want, list)
}
