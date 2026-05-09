package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/gmmakoele-ship-it/rafikiclaw/pkg/webhook"
)

func runWebhookServer(args []string) int {
	args = reorderFlags(args, map[string]bool{
		"--listen":      true,
		"--api-key":     true,
		"--events-file": true,
		"--strict":      false,
	})

	fs := flag.NewFlagSet("webhook server", flag.ContinueOnError)
	var listenAddr string
	var apiKey string
	var eventsFile string
	var strict bool

	fs.StringVar(&listenAddr, "listen", ":9393", "address to listen on")
	fs.StringVar(&apiKey, "api-key", "", "Bearer token for request authentication")
	fs.StringVar(&eventsFile, "events-file", "", "JSONL file to append all received events to")
	fs.BoolVar(&strict, "strict", false, "require valid API key for all requests")

	if err := fs.Parse(args); err != nil {
		return 1
	}

	opts := []webhook.ServerOption{}
	if apiKey != "" {
		opts = append(opts, webhook.WithServerAPIKey(apiKey))
	}
	if eventsFile != "" {
		opts = append(opts, webhook.WithEventsFile(eventsFile))
	}
	if strict {
		opts = append(opts, webhook.WithStrictAuth())
	}

	srv := webhook.NewServer(listenAddr, opts...)

	// Register default handler that logs all received events
	srv.RegisterHandler("*", func(ev webhook.Event) error {
		fmt.Printf("[webhook] received: type=%s agent=%s id=%s\n",
			ev.Type, ev.AgentID, ev.ID)
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
		_ = srv.Shutdown(context.Background())
	}()

	fmt.Printf("rafikiclaw webhook server listening on %s\n", listenAddr)
	if eventsFile != "" {
		fmt.Printf("  events log: %s\n", eventsFile)
	}
	if err := srv.Start(ctx); err != nil && err != context.Canceled {
		fmt.Fprintf(os.Stderr, "webhook server error: %v\n", err)
		return 1
	}
	return 0
}
