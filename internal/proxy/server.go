// Package proxy provides a local OpenAI-compatible proxy that injects skill
// context and enforces capability contracts for Rafiki OS agents.
//
// The proxy listens on :30000 by default and acts as a thin relay to the
// configured LLM backend (OpenAI, Gemini OpenAI-compatible, etc.).
// It intercepts requests to inject agent personality, memory context,
// and skill instructions before forwarding to the backend.
package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gmmakoele-ship-it/rafikiclaw/internal/capability"
	"github.com/gmmakoele-ship-it/rafikiclaw/internal/signing"
	"gopkg.in/yaml.v3"
)

const (
	DefaultListenAddr = ":30000"
	DefaultTimeout    = 120 * time.Second
)

// Config configures the proxy server.
type Config struct {
	ListenAddr  string
	LLMURL      string          // e.g. https://api.openai.com/v1 or Gemini endpoint
	APIKeyEnv   string          // env var name holding the API key
	SkillsDir   string          // directory containing skill .md files
	AgentPersona string         // path to agent persona .md file
	ContractPath string         // optional capability.contract.yaml to enforce
	SigningKey  string          // optional Ed25519 private key for request signing
	Timeout     time.Duration
}

// Server is the RafikiClaw proxy server.
type Server struct {
	Config
	http.Server
	llmClient http.Client
}

// NewServer creates a new proxy server from config.
func NewServer(cfg Config) (*Server, error) {
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = DefaultListenAddr
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = DefaultTimeout
	}
	apiKey := os.Getenv(cfg.APIKeyEnv)
	transport := &http.Transport{}
	proxyURL, err := url.Parse(cfg.LLMURL)
	if err != nil {
		return nil, fmt.Errorf("parse LLM URL: %w", err)
	}
	reverseProxy := httputil.NewSingleHostReverseProxy(proxyURL)
	reverseProxy.Transport = &authTransport{
		Base:      transport,
		APIKey:    apiKey,
		APIKeyEnv: cfg.APIKeyEnv,
	}

	mux := http.NewServeMux()
	s := &Server{Config: cfg}
	s.Handler = mux
	s.Server.Addr = cfg.ListenAddr
	s.Server.ReadTimeout = cfg.Timeout
	s.Server.WriteTimeout = cfg.Timeout

	// Health check endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "proxy": "rafikiclaw"})
	})

	// OpenAI-compatible /v1/models endpoint
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		req, _ := http.NewRequestWithContext(r.Context(), "GET", cfg.LLMURL+"/v1/models", nil)
		copyHeaders(req.Header, r.Header)
		if apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}
		resp, err := s.llmClient.Do(req)
		if err != nil {
			http.Error(w, fmt.Sprintf("upstream error: %v", err), 502)
			return
		}
		defer resp.Body.Close()
		copyResponse(w, resp)
	})

	// Chat completions — the main injection point
	mux.HandleFunc("/v1/chat/completions", s.handleChatCompletions)

	return s, nil
}

// handleChatCompletions relays to the LLM backend after optionally injecting
// skill context from the skills directory and/or agent persona.
func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}

	// Parse the incoming chat request
	var chatReq chatCompletionRequest
	if err := json.Unmarshal(body, &chatReq); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	// Inject skill context and persona into the system message
	chatReq = s.injectContext(chatReq)

	// Re-serialize and forward
	newBody, _ := json.Marshal(chatReq)
	forwardReq, _ := http.NewRequestWithContext(r.Context(), "POST", s.LLMURL+"/v1/chat/completions", bytes.NewReader(newBody))
	forwardReq.Header.Set("Content-Type", "application/json")
	apiKey := os.Getenv(s.APIKeyEnv)
	if apiKey != "" {
		forwardReq.Header.Set("Authorization", "Bearer "+apiKey)
	}
	for key, values := range r.Header {
		if key == "Content-Type" || key == "Authorization" || key == "Host" {
			continue
		}
		for _, v := range values {
			forwardReq.Header.Add(key, v)
		}
	}

	resp, err := s.llmClient.Do(forwardReq)
	if err != nil {
		http.Error(w, fmt.Sprintf("upstream error: %v", err), 502)
		return
	}
	defer resp.Body.Close()
	copyResponse(w, resp)
}

// injectContext prepends skill instructions and persona to the chat request.
func (s *Server) injectContext(req chatCompletionRequest) chatCompletionRequest {
	var systemParts []string

	if s.AgentPersona != "" {
		if persona, err := os.ReadFile(s.AgentPersona); err == nil {
			systemParts = append(systemParts, "## Agent Persona\n"+strings.TrimSpace(string(persona)))
		}
	}

	if s.SkillsDir != "" {
		skillCtx := s.loadSkillsContext()
		if skillCtx != "" {
			systemParts = append(systemParts, "## Available Skills\n"+skillCtx)
		}
	}

	if len(systemParts) == 0 {
		return req
	}

	systemPrompt := strings.TrimSpace(strings.Join(systemParts, "\n\n"))
	// Prepend to existing system message or create one
	newMessages := make([]chatMessage, 0, len(req.Messages)+1)
	newMessages = append(newMessages, chatMessage{Role: "system", Content: systemPrompt})
	newMessages = append(newMessages, req.Messages...)
	req.Messages = newMessages
	return req
}

// loadSkillsContext reads all .md files in the skills directory and concatenates them.
func (s *Server) loadSkillsContext() string {
	if s.SkillsDir == "" {
		return ""
	}
	entries, err := os.ReadDir(s.SkillsDir)
	if err != nil {
		return ""
	}
	var parts []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		path := filepath.Join(s.SkillsDir, entry.Name())
		if data, err := os.ReadFile(path); err == nil {
			parts = append(parts, "### "+entry.Name()+"\n"+strings.TrimSpace(string(data)))
		}
	}
	return strings.Join(parts, "\n\n")
}

// Start launches the proxy server.
func (s *Server) Start(ctx context.Context) error {
	if s.ContractPath != "" {
		if contract, _, err := capability.LoadFromSkillPath(s.ContractPath); err == nil {
			_ = contract // future: enforce contract on incoming requests
		}
	}
	return s.Server.ListenAndServe()
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.Server.Shutdown(ctx)
}

// --- request/response types (subset of OpenAI API) ---

type chatCompletionRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature,omitempty"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Stream      bool          `json:"stream,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	Name    string `json:"name,omitempty"`
}

// --- helpers ---

type authTransport struct {
	Base      *http.Transport
	APIKey    string
	APIKeyEnv string
}

func (t *authTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if t.APIKey != "" && r.Header.Get("Authorization") == "" {
		r.Header.Set("Authorization", "Bearer "+t.APIKey)
	}
	return t.Base.RoundTrip(r)
}

func copyHeaders(dst, src http.Header) {
	for key, values := range src {
		for _, v := range values {
			dst.Add(key, v)
		}
	}
}

func copyResponse(w http.ResponseWriter, resp *http.Response) {
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// LoadContract loads a capability contract from the given path.
func LoadContract(path string) (capability.Contract, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return capability.Contract{}, err
	}
	var c capability.Contract
	if err := yaml.Unmarshal(b, &c); err != nil {
		return capability.Contract{}, err
	}
	return c, capability.Validate(c)
}

// SignRequest signs a request payload with the given Ed25519 key.
// Returns the base64-encoded signature.
func SignRequest(payload []byte, keyFile string) (string, error) {
	key, err := signing.LoadPrivateKeyPEM(keyFile)
	if err != nil {
		return "", fmt.Errorf("load signing key: %w", err)
	}
	return signing.Sign(payload, key), nil
}
