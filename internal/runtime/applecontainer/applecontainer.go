package applecontainer

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/gmmakoele-ship-it/rafikiclaw/internal/policy"
	"github.com/gmmakoele-ship-it/rafikiclaw/internal/runtime/spec"
)

type Adapter struct {
	bin string
}

func New() *Adapter {
	bin := os.Getenv("METACLAW_APPLE_CONTAINER_BIN")
	if bin == "" {
		bin = "container"
	}
	return &Adapter{bin: bin}
}

func (a *Adapter) Name() spec.Target { return spec.TargetApple }

func (a *Adapter) Available(context.Context) bool {
	_, err := exec.LookPath(a.bin)
	return err == nil
}

func (a *Adapter) Run(ctx context.Context, opts spec.RunOptions) (spec.RunResult, error) {
	args := []string{"run", "--name", opts.ContainerName}
	if opts.Detach {
		args = append(args, "-d")
	}
	args = append(args, policyFlags(opts.Policy, opts.Env, opts.Workdir, opts.User, opts.CPU, opts.Memory)...)
	args = append(args, opts.Image)
	args = append(args, opts.Command...)
	stdout, stderr, code, err := run(ctx, a.bin, args, opts.Env)
	if opts.Detach {
		return spec.RunResult{ContainerID: strings.TrimSpace(stdout), ExitCode: code, Stdout: stdout, Stderr: stderr}, err
	}
	return spec.RunResult{ContainerID: opts.ContainerName, ExitCode: code, Stdout: stdout, Stderr: stderr}, err
}

func (a *Adapter) Logs(ctx context.Context, containerID string, follow bool) (string, error) {
	args := []string{"logs"}
	if follow {
		args = append(args, "--follow")
	}
	args = append(args, containerID)
	stdout, stderr, _, err := run(ctx, a.bin, args, nil)
	if err != nil {
		return stdout + stderr, err
	}
	return stdout + stderr, nil
}

func (a *Adapter) Inspect(ctx context.Context, containerID string) (string, error) {
	stdout, stderr, _, err := run(ctx, a.bin, []string{"inspect", containerID}, nil)
	if err != nil {
		return stdout + stderr, err
	}
	return stdout, nil
}

func (a *Adapter) ExecShell(ctx context.Context, containerID string) error {
	cmd := exec.CommandContext(ctx, a.bin, "exec", "-it", containerID, "sh")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("shell session ended with non-zero exit: %w", err)
		}
		return err
	}
	return nil
}

func (a *Adapter) Remove(ctx context.Context, containerID string) error {
	_, _, _, err := run(ctx, a.bin, []string{"rm", "-f", containerID}, nil)
	return err
}

func policyFlags(p policy.Policy, env map[string]string, workdir, user, cpu, memory string) []string {
	args := make([]string, 0)
	switch p.Network.Mode {
	case "none":
		args = append(args, "--network=none")
	case "outbound":
		args = append(args, "--network=bridge")
	case "all":
		args = append(args, "--network=host")
	}
	for _, m := range p.Mounts {
		v := fmt.Sprintf("%s:%s", m.Source, m.Target)
		if m.ReadOnly {
			v += ":ro"
		}
		args = append(args, "-v", v)
	}
	allow := make(map[string]struct{}, len(p.EnvAllowlist))
	for _, k := range p.EnvAllowlist {
		allow[k] = struct{}{}
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		if _, ok := allow[k]; ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		args = append(args, "-e", k)
	}
	if workdir != "" {
		args = append(args, "-w", workdir)
	}
	if user != "" {
		args = append(args, "-u", user)
	}
	if cpu != "" {
		args = append(args, "--cpus", cpu)
	}
	if memory != "" {
		args = append(args, "--memory", memory)
	}
	return args
}

func run(ctx context.Context, bin string, args []string, extraEnv map[string]string) (string, string, int, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = mergeEnv(extraEnv)
	var out bytes.Buffer
	var errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	err := cmd.Run()
	exit := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			exit = ee.ExitCode()
		} else {
			exit = -1
		}
	}
	return out.String(), errBuf.String(), exit, err
}

func mergeEnv(extra map[string]string) []string {
	if len(extra) == 0 {
		return os.Environ()
	}
	env := make(map[string]string, len(extra)+8)
	for _, item := range os.Environ() {
		if i := strings.IndexByte(item, '='); i > 0 {
			env[item[:i]] = item[i+1:]
		}
	}
	for k, v := range extra {
		env[k] = v
	}
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	sort.Strings(out)
	return out
}
