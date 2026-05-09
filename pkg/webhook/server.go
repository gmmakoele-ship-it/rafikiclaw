package webhook

import (
	"bufio"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Server receives webhook events and dispatches them to registered handlers.
type Server struct {
	Addr           string
	APIKey         string
	handlers       map[string]EventHandler
	handlersMu     sync.RWMutex
	httpServer      http.Server
	eventsFile     string
	strictAuth     bool
}

type eventHandlerEntry struct {
	handler EventHandler
	filter  string // optional: match on event.Type
}

type eventPayload struct {
	Type    string                 `json:"type"`
	AgentID string                 `json:"agentId"`
	Payload map[string]interface{} `json:"payload"`
}

// EventHandler processes a received webhook event.
// Return nil to acknowledge, or an error to reject with an HTTP error response.
type EventHandler func(Event) error

// ServerOption configures the Server.
type ServerOption func(*Server)

// WithServerAPIKey sets the expected Authorization Bearer token.
func WithServerAPIKey(key string) ServerOption {
	return func(s *Server) { s.APIKey = key }
}

// WithEventsFile appends all received events to a JSONL file for audit.
func WithEventsFile(path string) ServerOption {
	return func(s *Server) { s.eventsFile = path }
}

// WithStrictAuth requires valid API key for all requests.
func WithStrictAuth() ServerOption {
	return func(s *Server) { s.strictAuth = true }
}

// NewServer creates a webhook server listening on addr.
func NewServer(addr string, opts ...ServerOption) *Server {
	s := &Server{
		Addr:     addr,
		handlers: make(map[string]EventHandler),
	}
	for _, o := range opts {
		o(s)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/events", s.handleEvents)
	mux.HandleFunc("/events/", s.handleEventByID)
	s.httpServer = http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}
	return s
}

// RegisterHandler registers a handler for a named event type.
// For example, RegisterHandler("task.complete", myHandler).
func (s *Server) RegisterHandler(eventType string, handler EventHandler) {
	s.handlersMu.Lock()
	defer s.handlersMu.Unlock()
	s.handlers[eventType] = handler
}

// Start begins listening. It blocks until ctx is cancelled.
func (s *Server) Start(ctx context.Context) error {
	s.log("webhook server listening on %s", s.Addr)
	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("webhook server: %w", err)
	}
	<-ctx.Done()
	return nil
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

func (s *Server) log(format string, args ...interface{}) {
	log.Printf("[webhook] "+format, args...)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.strictAuth || s.APIKey != "" {
		if !s.validateAuth(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}

	var ev Event
	if err := json.Unmarshal(body, &ev); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	s.dispatchEvent(ev)
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"received": ev.ID})
}

func (s *Server) handleEventByID(w http.ResponseWriter, r *http.Request) {
	// GET /events/:id — retrieve a specific event from the audit log
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/events/")
	if id == "" {
		http.Error(w, "event id required", http.StatusBadRequest)
		return
	}
	if s.eventsFile != "" {
		ev, found := s.findEventByID(id)
		if !found {
			http.Error(w, "event not found", http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(ev)
		return
	}
	http.Error(w, "event lookup not enabled", http.StatusNotFound)
}

func (s *Server) validateAuth(r *http.Request) bool {
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return false
	}
	token := strings.TrimPrefix(header, "Bearer ")
	// Constant-time comparison to avoid timing attacks
	expected := []byte(s.APIKey)
	actual := []byte(token)
	if len(expected) != len(actual) {
		return false
	}
	var diff byte
	for i := 0; i < len(expected); i++ {
		diff |= expected[i] ^ actual[i]
	}
	return diff == 0
}

func (s *Server) dispatchEvent(ev Event) {
	if s.eventsFile != "" {
		s.appendEvent(ev)
	}

	s.handlersMu.RLock()
	handler, ok := s.handlers[ev.Type]
	s.handlersMu.RUnlock()

	if !ok {
		s.log("no handler registered for event type: %s", ev.Type)
		return
	}

	if err := handler(ev); err != nil {
		s.log("handler error for %s [%s]: %v", ev.Type, ev.ID, err)
	}
}

func (s *Server) appendEvent(ev Event) error {
	if s.eventsFile == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.eventsFile), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(s.eventsFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	return enc.Encode(ev)
}

func (s *Server) findEventByID(id string) (Event, bool) {
	if s.eventsFile == "" {
		return Event{}, false
	}
	f, err := os.Open(s.eventsFile)
	if err != nil {
		return Event{}, false
	}
	defer f.Close()
	r := bufio.NewReader(f)
	for {
		line, err := r.ReadString('\n')
		if err == io.EOF && line == "" {
			break
		}
		if err != nil {
			break
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var ev Event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		if ev.ID == id {
			return ev, true
		}
	}
	return Event{}, false
}

// SignEvent computes an HMAC-SHA256 signature over the event payload.
func SignEvent(event Event, secret string) string {
	data, _ := json.Marshal(event.Payload)
	h := hmac.New(sha256.New, []byte(secret))
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}
