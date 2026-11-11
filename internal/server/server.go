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
	"net"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	"gomini/internal/gemini"
)

const version = "1.1.0"

// GeminiClient defines generation methods required by the HTTP server.
type GeminiClient interface {
	Generate(ctx context.Context, prompt string, modelID, thinkMode int, fileRefs []string, extra map[int]int, ticketHeader ...string) (string, error)
	GenerateStream(ctx context.Context, prompt string, modelID, thinkMode int, fileRefs []string, extra map[int]int, ticketHeader ...string) <-chan gemini.StreamEvent
	SetLogger(func(string))
}

// Uploader defines image upload operations.
type Uploader interface {
	UploadImage(ctx context.Context, imageBytes []byte, mimeType string) (string, error)
	FetchImageBytes(ctx context.Context, urlStr string) ([]byte, error)
}

// Config holds server configuration settings.
type Config struct {
	APIKeys         []string
	DefaultModel    string
	LogRequests     bool
	SendModelTicket bool
	ModelTickets    map[string]string
	CookieFile      *string
	CookieStore     *gemini.CookieStore
}

// NewConfig returns a default server configuration.
func NewConfig(apiKeys []string, defaultModel string, logReqs bool) *Config {
	return &Config{
		APIKeys:         apiKeys,
		DefaultModel:    defaultModel,
		LogRequests:     logReqs,
		SendModelTicket: true,
		ModelTickets:    make(map[string]string),
	}
}

// Server is the HTTP server for OpenAI and Gemini proxy endpoints.
type Server struct {
	config   *Config
	gemini   GeminiClient
	uploader Uploader
	mux      *http.ServeMux
}

// New creates and initializes an HTTP API server.
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
	if s.config.LogRequests {
		clientIP := r.RemoteAddr
		if host, _, err := net.SplitHostPort(clientIP); err == nil {
			clientIP = host
		}
		log.Printf("%s %s %s", clientIP, r.Method, r.URL.Path)
	}
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /v1/models", s.withMiddleware(s.handleModels))
	s.mux.HandleFunc("GET /v1beta/models", s.withMiddleware(s.handleGoogleModels))
	s.mux.HandleFunc("POST /v1/chat/completions", s.withMiddleware(s.handleChat))
	s.mux.HandleFunc("POST /v1/responses", s.withMiddleware(s.handleResponses))
	s.mux.HandleFunc("POST /v1beta/models/", s.withMiddleware(s.handleGoogleV1beta))
	s.mux.HandleFunc("POST /v1/cookies", s.handleCookiesSync)
	s.mux.HandleFunc("GET /", s.handleHealth)
	s.mux.HandleFunc("GET /health", s.handleHealth)
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

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"version": version,
	})
}

var re16Hex = regexp.MustCompile(`^[0-9a-fA-F]{16}$`)

func (s *Server) handleCookiesSync(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if host != "127.0.0.1" && host != "localhost" {
		writeJSON(w, http.StatusForbidden, errorObj("forbidden: local access only"))
		return
	}
	origin := r.Header.Get("Origin")
	syncHeader := r.Header.Get("X-Gemini-Cookie-Sync")
	if syncHeader != "1" && !strings.HasPrefix(origin, "chrome-extension://") {
		writeJSON(w, http.StatusForbidden, errorObj("forbidden: extension auth required"))
		return
	}

	var payload struct {
		Cookie       string            `json:"cookie"`
		Sapisid      string            `json:"sapisid"`
		AuthUser     any               `json:"auth_user"`
		ModelTickets map[string]string `json:"model_tickets"`
	}
	if err := parseBody(r, &payload); err != nil {
		writeJSON(w, http.StatusBadRequest, errorObj(err.Error()))
		return
	}
	if strings.ContainsAny(payload.Cookie, "\r\n") {
		writeJSON(w, http.StatusBadRequest, errorObj("invalid cookie: newline detected"))
		return
	}

	if s.config.CookieStore != nil && payload.Cookie != "" {
		s.config.CookieStore.Set(payload.Cookie, payload.Sapisid)
	}

	if payload.ModelTickets != nil {
		if s.config.ModelTickets == nil {
			s.config.ModelTickets = make(map[string]string)
		}
		for k, v := range payload.ModelTickets {
			v = strings.ToLower(strings.TrimSpace(v))
			if re16Hex.MatchString(v) && strings.TrimSpace(k) != "" {
				s.config.ModelTickets[k] = v
			}
		}
	}

	if s.config.CookieFile != nil && *s.config.CookieFile != "" {
		fPath := *s.config.CookieFile
		existingData := make(map[string]any)
		if content, err := os.ReadFile(fPath); err == nil && len(content) > 0 && content[0] == '{' {
			_ = json.Unmarshal(content, &existingData)
		}
		if payload.Cookie != "" {
			existingData["cookie"] = payload.Cookie
		}
		if payload.Sapisid != "" {
			existingData["sapisid"] = payload.Sapisid
		}
		if payload.AuthUser != nil {
			existingData["auth_user"] = payload.AuthUser
		}
		if len(s.config.ModelTickets) > 0 {
			existingData["model_tickets"] = s.config.ModelTickets
		}
		if b, err := json.MarshalIndent(existingData, "", "  "); err == nil {
			_ = os.WriteFile(fPath, b, 0600)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) handleCORS(w http.ResponseWriter, _ *http.Request) {
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
