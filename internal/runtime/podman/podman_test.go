package podman

import (
	"testing"

	"github.com/gmmakoele-ship-it/rafikiclaw/internal/policy"
)

func TestPolicyFlagsUseEnvKeysWithoutInliningSecrets(t *testing.T) {
	p := policy.Policy{
		Network: policy.NetworkPolicy{Mode: "all", Allowed: true},
		EnvAllowlist: []string{
			"GEMINI_API_KEY",
		},
	}
	env := map[string]string{
		"GEMINI_API_KEY": "top-secret",
	}

	args := policyFlags(p, env, "", "", "2", "1g")
	if contains(args, "GEMINI_API_KEY=top-secret") {
		t.Fatalf("env value leaked into args: %v", args)
	}
	if !containsPair(args, "-e", "GEMINI_API_KEY") {
		t.Fatalf("missing -e GEMINI_API_KEY in args: %v", args)
	}
	if !contains(args, "--network=host") {
		t.Fatalf("expected all to map to host network: %v", args)
	}
	if !containsPair(args, "--cpus", "2") || !containsPair(args, "--memory", "1g") {
		t.Fatalf("missing resource flags in args: %v", args)
	}
}

func contains(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

func containsPair(args []string, left, right string) bool {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == left && args[i+1] == right {
			return true
		}
	}
	return false
}
