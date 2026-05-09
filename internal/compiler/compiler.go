package compiler

import (
	"fmt"

	"github.com/gmmakoele-ship-it/rafikiclaw/internal/capsule"
	"github.com/gmmakoele-ship-it/rafikiclaw/internal/claw/parse"
	v1 "github.com/gmmakoele-ship-it/rafikiclaw/internal/claw/schema/v1"
	"github.com/gmmakoele-ship-it/rafikiclaw/internal/claw/validate"
	"github.com/gmmakoele-ship-it/rafikiclaw/internal/locks"
	"github.com/gmmakoele-ship-it/rafikiclaw/internal/policy"
)

type Result struct {
	Config  v1.Clawfile
	Policy  policy.Policy
	Locks   locks.BundleLocks
	Capsule capsule.Capsule
}

func LoadNormalize(path string) (v1.Clawfile, error) {
	cfg, err := parse.File(path)
	if err != nil {
		return v1.Clawfile{}, err
	}
	n, err := validate.NormalizeAndValidate(cfg, path)
	if err != nil {
		return v1.Clawfile{}, err
	}
	return n, nil
}

func Compile(path string, outputDir string) (Result, error) {
	normalized, err := LoadNormalize(path)
	if err != nil {
		return Result{}, err
	}
	pol, err := policy.Compile(normalized)
	if err != nil {
		return Result{}, err
	}
	lk, err := locks.Generate(normalized, path, outputDir)
	if err != nil {
		return Result{}, err
	}

	ir := map[string]any{
		"version":  "rafikiclaw.ir/v1",
		"clawfile": normalized,
		"runtime": map[string]any{
			"target": normalized.Agent.Runtime.Target,
			"image":  normalized.Agent.Runtime.Image,
		},
		// Keep this stable so absolute vs relative compile paths produce identical capsules.
		"sourceRoot": ".",
	}

	cap, err := capsule.Write(outputDir, path, ir, pol, lk)
	if err != nil {
		return Result{}, fmt.Errorf("write capsule: %w", err)
	}
	return Result{Config: normalized, Policy: pol, Locks: lk, Capsule: cap}, nil
}
