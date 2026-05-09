package runtime

import (
	"context"
	"fmt"
	goruntime "runtime"

	"github.com/gmmakoele-ship-it/rafikiclaw/internal/runtime/applecontainer"
	"github.com/gmmakoele-ship-it/rafikiclaw/internal/runtime/docker"
	"github.com/gmmakoele-ship-it/rafikiclaw/internal/runtime/podman"
	"github.com/gmmakoele-ship-it/rafikiclaw/internal/runtime/spec"
)

type Resolver struct {
	adapters map[spec.Target]spec.Adapter
}

func NewResolver() *Resolver {
	return &Resolver{adapters: map[spec.Target]spec.Adapter{
		spec.TargetPodman: podman.New(),
		spec.TargetApple:  applecontainer.New(),
		spec.TargetDocker: docker.New(),
	}}
}

func ParseTarget(v string) (spec.Target, error) {
	switch spec.Target(v) {
	case "", spec.TargetPodman, spec.TargetApple, spec.TargetDocker:
		return spec.Target(v), nil
	default:
		return "", fmt.Errorf("invalid runtime target: %s", v)
	}
}

func (r *Resolver) Resolve(ctx context.Context, cliOverride string, clawfileTarget string) (spec.Adapter, spec.Target, error) {
	if cliOverride != "" {
		t, err := ParseTarget(cliOverride)
		if err != nil {
			return nil, "", err
		}
		ad, ok := r.adapters[t]
		if !ok || !ad.Available(ctx) {
			return nil, "", fmt.Errorf("runtime %s is not available on this host", cliOverride)
		}
		return ad, t, nil
	}

	if clawfileTarget != "" {
		t, err := ParseTarget(clawfileTarget)
		if err != nil {
			return nil, "", err
		}
		ad, ok := r.adapters[t]
		if !ok || !ad.Available(ctx) {
			return nil, "", fmt.Errorf("runtime %s declared in clawfile is not available", clawfileTarget)
		}
		return ad, t, nil
	}

	defaultOrder := hostDefaultOrder()
	for _, t := range defaultOrder {
		if ad, ok := r.adapters[t]; ok && ad.Available(ctx) {
			return ad, t, nil
		}
	}
	return nil, "", fmt.Errorf("no supported runtime available; install podman, docker, or apple container")
}

func hostDefaultOrder() []spec.Target {
	if goruntime.GOOS == "darwin" {
		return []spec.Target{spec.TargetApple, spec.TargetDocker, spec.TargetPodman}
	}
	return []spec.Target{spec.TargetPodman, spec.TargetDocker, spec.TargetApple}
}

func (r *Resolver) Adapter(target spec.Target) (spec.Adapter, bool) {
	ad, ok := r.adapters[target]
	return ad, ok
}
