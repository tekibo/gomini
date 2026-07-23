// Package server implements the HTTP API layer with OpenAI-compatible
// endpoints, CORS, and authentication middleware.
package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"slices"
	"strings"
	"time"

	"gomini/internal/gemini"
)

const version = "1.1.0"

type GeminiClient interface {
	Generate(ctx context.Context, prompt string, modelID, thinkMode int, fileRefs []string, extra map[int]int) (string, error)
	GenerateStream(ctx context.Context, prompt string, modelID, thinkMode int, fileRefs []string, extra map[int]int) <-chan gemini.StreamEvent
	SetLogger(func(string))
}

type Uploader interface {
	UploadImage(ctx context.Context, imageBytes []byte, mimeType string) (string, error)
}

type Config struct {
	APIKeys       []string
	DefaultModel  string
	LogRequests   bool
}

func NewConfig(apiKeys []string, defaultModel string, logReqs bool) *Config {
	return &Config{
		APIKeys:      apiKeys,
		DefaultModel: defaultModel,
		LogRequests:  logReqs,
	}
}

type Server struct {
	config   *Config
	gemini   GeminiClient
	uploader Uploader
	mux      *http.ServeMux
}

func New(cfg *Config, geminiClient GeminiClient, uploader Uploader) *Server {
	s := &Server{
		config:   cfg,
		gemini:   geminiClient,
		uploader: uploader,
		mux:      http.NewServeMux(),
	}
	if cfg.LogRequests {
		geminiClient.SetLogger(func(msg string) {
			log.Printf("[%s] %s", time.Now().Format("15:04:05"), msg)
		})
	}
	s.routes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /v1/models", s.withMiddleware(s.handleModels))
	s.mux.HandleFunc("GET /v1beta/models", s.withMiddleware(s.handleGoogleModels))
	s.mux.HandleFunc("POST /v1/chat/completions", s.withMiddleware(s.handleChat))
	s.mux.HandleFunc("POST /v1/responses", s.withMiddleware(s.handleResponses))
	s.mux.HandleFunc("POST /v1beta/models/", s.withMiddleware(s.handleGoogleV1beta))
	s.mux.HandleFunc("GET /", s.handleHealth)
	s.mux.HandleFunc("OPTIONS /", s.handleCORS)
}

func (s *Server) withMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return s.cors(s.auth(next))
}

func (s *Server) cors(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "*")
		next(w, r)
	}
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/v1/") && !strings.HasPrefix(r.URL.Path, "/v1beta/") {
			next(w, r)
			return
		}
		if len(s.config.APIKeys) == 0 {
			next(w, r)
			return
		}
		auth := r.Header.Get("Authorization")
		key, ok := strings.CutPrefix(auth, "Bearer ")
		if !ok {
			key = r.Header.Get("x-api-key")
		}
		if key == "" {
			writeJSON(w, http.StatusUnauthorized, errorObj("invalid api key"))
			return
		}
		if !slices.Contains(s.config.APIKeys, key) {
			writeJSON(w, http.StatusUnauthorized, errorObj("invalid api key"))
			return
		}
		next(w, r)
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"version": version,
	})
}

func (s *Server) handleCORS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "*")
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func startSSE(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
}

func writeSSE(w http.ResponseWriter, data any) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", b)
	return err
}

func writeSSEEvent(w http.ResponseWriter, event string, data any) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
	return err
}

func errorObj(msg string) map[string]any {
	return map[string]any{
		"error": map[string]string{"message": msg},
	}
}

func parseBody(r *http.Request, v any) error {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return nil
}

func usageTokens(s string) int {
	return len(s) / 4
}

func hexID(n int) string {
	b := make([]byte, n/2+1)
	rand.Read(b)
	return hex.EncodeToString(b)[:n]
}
