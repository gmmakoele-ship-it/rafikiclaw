package cli

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func runDashboardCmd(args []string) int {
	args = reorderFlags(args, map[string]bool{"--state-dir": true})

	fs := flag.NewFlagSet("dashboard", flag.ContinueOnError)
	var stateDir string
	fs.StringVar(&stateDir, "state-dir", ".rafikiclaw", "rafikiclaw state directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	// Find the dashboard binary next to the running executable
	exePath, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "rafikiclaw dashboard: cannot find executable: %v\n", err)
		return 1
	}
	exeDir := filepath.Dir(exePath)
	dashBin := filepath.Join(exeDir, "dashboard")

	// Fallback: look in ./bin/
	if _, err := os.Stat(dashBin); os.IsNotExist(err) {
		dashBin = filepath.Join(exeDir, "..", "bin", "dashboard")
	}

	cmd := exec.Command(dashBin, "--state-dir", stateDir)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			os.Exit(exit.ExitCode())
		}
		fmt.Fprintf(os.Stderr, "rafikiclaw dashboard: %v\n", err)
		return 1
	}
	return 0
}