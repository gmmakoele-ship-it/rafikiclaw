package spec

import (
	"context"

	"github.com/gmmakoele-ship-it/rafikiclaw/internal/policy"
)

type Target string

const (
	TargetPodman Target = "podman"
	TargetApple  Target = "apple_container"
	TargetDocker Target = "docker"
)

type RunOptions struct {
	ContainerName string
	Image         string
	Command       []string
	Detach        bool
	Policy        policy.Policy
	Env           map[string]string
	Workdir       string
	User          string
	CPU           string
	Memory        string
}

type RunResult struct {
	ContainerID string
	ExitCode    int
	Stdout      string
	Stderr      string
}

type Adapter interface {
	Name() Target
	Available(ctx context.Context) bool
	Run(ctx context.Context, opts RunOptions) (RunResult, error)
	Logs(ctx context.Context, containerID string, follow bool) (string, error)
	Inspect(ctx context.Context, containerID string) (string, error)
	ExecShell(ctx context.Context, containerID string) error
	Remove(ctx context.Context, containerID string) error
}
