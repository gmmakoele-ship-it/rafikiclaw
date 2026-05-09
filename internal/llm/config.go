package llm

import (
	"fmt"
	"os"
	"sort"
	"strings"

	v1 "github.com/gmmakoele-ship-it/rafikiclaw/internal/claw/schema/v1"
)

type RuntimeOptions struct {
	APIKey    string
	APIKeyEnv string
}

type Resolved struct {
	Enabled bool
	Env     map[string]string
}

func Resolve(spec v1.LLMSpec, opts RuntimeOptions) (Resolved, error) {
	if spec.Provider == "" {
		return Resolved{Enabled: false, Env: map[string]string{}}, nil
	}

	key := strings.TrimSpace(opts.APIKey)
	if key == "" && strings.TrimSpace(opts.APIKeyEnv) != "" {
		key = strings.TrimSpace(os.Getenv(strings.TrimSpace(opts.APIKeyEnv)))
		if key == "" {
			return Resolved{}, fmt.Errorf("host env %s is empty", strings.TrimSpace(opts.APIKeyEnv))
		}
	}
	if key == "" {
		key = strings.TrimSpace(os.Getenv(spec.APIKeyEnv))
	}
	if key == "" {
		return Resolved{}, fmt.Errorf("missing LLM API key: set --llm-api-key, --llm-api-key-env, or host env %s", spec.APIKeyEnv)
	}

	env := map[string]string{
		spec.APIKeyEnv:          key,
		"METACLAW_LLM_PROVIDER": string(spec.Provider),
		"METACLAW_LLM_MODEL":    spec.Model,
	}
	if spec.BaseURL != "" {
		env["METACLAW_LLM_BASE_URL"] = spec.BaseURL
	}

	switch spec.Provider {
	case v1.LLMProviderOpenAICompatible, v1.LLMProviderGeminiOpenAI:
		env["OPENAI_API_KEY"] = key
		if spec.BaseURL != "" {
			env["OPENAI_BASE_URL"] = spec.BaseURL
		}
	case v1.LLMProviderAnthropic:
		// Some bots talk to Anthropic directly (not OpenAI-compatible).
		env["ANTHROPIC_API_KEY"] = key
		if spec.BaseURL != "" {
			env["ANTHROPIC_BASE_URL"] = spec.BaseURL
		}
	}
	if spec.Provider == v1.LLMProviderGeminiOpenAI {
		env["GEMINI_API_KEY"] = key
	}

	return Resolved{Enabled: true, Env: env}, nil
}

func AllowedEnvKeys(spec v1.LLMSpec) []string {
	if spec.Provider == "" {
		return nil
	}
	keySet := map[string]struct{}{
		spec.APIKeyEnv:          {},
		"METACLAW_LLM_PROVIDER": {},
		"METACLAW_LLM_MODEL":    {},
	}
	if spec.BaseURL != "" {
		keySet["METACLAW_LLM_BASE_URL"] = struct{}{}
	}
	switch spec.Provider {
	case v1.LLMProviderOpenAICompatible, v1.LLMProviderGeminiOpenAI:
		keySet["OPENAI_API_KEY"] = struct{}{}
		if spec.BaseURL != "" {
			keySet["OPENAI_BASE_URL"] = struct{}{}
		}
	case v1.LLMProviderAnthropic:
		keySet["ANTHROPIC_API_KEY"] = struct{}{}
		if spec.BaseURL != "" {
			keySet["ANTHROPIC_BASE_URL"] = struct{}{}
		}
	}
	if spec.Provider == v1.LLMProviderGeminiOpenAI {
		keySet["GEMINI_API_KEY"] = struct{}{}
	}
	keys := make([]string, 0, len(keySet))
	for k := range keySet {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
