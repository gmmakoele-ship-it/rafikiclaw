package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/gmmakoele-ship-it/rafikiclaw/internal/proxy"
)

func runProxy(args []string) int {
	args = reorderFlags(args, map[string]bool{
		"--listen":       true,
		"--llm-url":      true,
		"--api-key-env":  true,
		"--skills-dir":   true,
		"--persona":      true,
		"--contract":     true,
		"--signing-key":  true,
		"--timeout":      true,
	})

	fs := flag.NewFlagSet("proxy", flag.ContinueOnError)
	var listenAddr string
	var llmURL string
	var apiKeyEnv string
	var skillsDir string
	var agentPersona string
	var contractPath string
	var signingKey string
	var timeoutSec int

	fs.StringVar(&listenAddr, "listen", ":30000", "address to listen on")
	fs.StringVar(&llmURL, "llm-url", "", "LLM backend URL (e.g. https://api.openai.com/v1)")
	fs.StringVar(&apiKeyEnv, "api-key-env", "OPENAI_API_KEY", "env variable holding LLM API key")
	fs.StringVar(&skillsDir, "skills-dir", "", "directory containing skill .md files to inject")
	fs.StringVar(&agentPersona, "persona", "", "path to agent persona .md file")
	fs.StringVar(&contractPath, "contract", "", "optional capability.contract.yaml path")
	fs.StringVar(&signingKey, "signing-key", "", "optional Ed25519 private key for request signing")
	fs.IntVar(&timeoutSec, "timeout", 120, "request timeout in seconds")

	if err := fs.Parse(args); err != nil {
		return 1
	}

	if llmURL == "" {
		fmt.Fprintln(os.Stderr, "rafikiclaw proxy: --llm-url is required")
		return 1
	}
	if strings.TrimSpace(llmURL) == "" {
		fmt.Fprintln(os.Stderr, "rafikiclaw proxy: --llm-url cannot be empty")
		return 1
	}

	cfg := proxy.Config{
		ListenAddr:   listenAddr,
		LLMURL:       llmURL,
		APIKeyEnv:    apiKeyEnv,
		SkillsDir:    skillsDir,
		AgentPersona: agentPersona,
		ContractPath: contractPath,
		SigningKey:   signingKey,
	}

	srv, err := proxy.NewServer(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "rafikiclaw proxy: failed to create server: %v\n", err)
		return 1
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		cancel()
		_ = srv.Shutdown(context.Background())
	}()

	fmt.Printf("rafikiclaw proxy listening on %s\n", listenAddr)
	fmt.Printf("  LLM backend: %s\n", llmURL)
	if skillsDir != "" {
		fmt.Printf("  skills dir: %s\n", skillsDir)
	}
	if agentPersona != "" {
		fmt.Printf("  persona: %s\n", agentPersona)
	}
	if contractPath != "" {
		fmt.Printf("  contract: %s\n", contractPath)
	}

	if err := srv.Start(ctx); err != nil && err != context.Canceled {
		fmt.Fprintf(os.Stderr, "rafikiclaw proxy: %v\n", err)
		return 1
	}
	return 0
}