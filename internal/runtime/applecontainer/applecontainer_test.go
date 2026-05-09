package applecontainer

import (
	"testing"

	"github.com/gmmakoele-ship-it/rafikiclaw/internal/policy"
)

func TestPolicyFlagsUseEnvKeysWithoutInliningSecrets(t *testing.T) {
	p := policy.Policy{
		Network: policy.NetworkPolicy{Mode: "none", Allowed: false},
		EnvAllowlist: []string{
			"OPENAI_API_KEY",
		},
	}
	env := map[string]string{
		"OPENAI_API_KEY": "not-in-args",
	}
	args := policyFlags(p, env, "/work", "", "0.5", "256m")

	if contains(args, "OPENAI_API_KEY=not-in-args") {
		t.Fatalf("env value leaked into args: %v", args)
	}
	if !containsPair(args, "-e", "OPENAI_API_KEY") {
		t.Fatalf("missing -e OPENAI_API_KEY in args: %v", args)
	}
	if !contains(args, "--network=none") {
		t.Fatalf("expected none network flag: %v", args)
	}
	if !containsPair(args, "--cpus", "0.5") || !containsPair(args, "--memory", "256m") {
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
