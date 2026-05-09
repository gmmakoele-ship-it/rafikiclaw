package docker

import (
	"testing"

	"github.com/gmmakoele-ship-it/rafikiclaw/internal/policy"
)

func TestPolicyFlagsUseEnvKeysWithoutInliningSecrets(t *testing.T) {
	p := policy.Policy{
		Network: policy.NetworkPolicy{Mode: "outbound", Allowed: true},
		Mounts: []policy.MountPolicy{
			{Source: "/host", Target: "/ctr", ReadOnly: true},
		},
		EnvAllowlist: []string{"FOO", "OPENAI_API_KEY"},
	}
	env := map[string]string{
		"OPENAI_API_KEY": "super-secret-value",
		"FOO":            "bar",
	}

	args := policyFlags(p, env, "/work", "1000:1000", "1.5", "512m")
	if contains(args, "OPENAI_API_KEY=super-secret-value") {
		t.Fatalf("env value leaked into args: %v", args)
	}
	if !containsPair(args, "-e", "OPENAI_API_KEY") {
		t.Fatalf("missing -e OPENAI_API_KEY in args: %v", args)
	}
	if !containsPair(args, "--cpus", "1.5") || !containsPair(args, "--memory", "512m") {
		t.Fatalf("missing resource flags in args: %v", args)
	}
	if !contains(args, "--network=bridge") {
		t.Fatalf("expected outbound to map to bridge network: %v", args)
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
