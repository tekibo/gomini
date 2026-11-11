// Command gomini starts the Gemini-to-OpenAI API proxy server.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gomini/internal/config"
	"gomini/internal/gemini"
	"gomini/internal/multimodal"
	"gomini/internal/server"
)

func main() {
	cfg := config.Default()
	cfg.ApplyEnv()
	cfg.ApplyFlags()

	geminiCfg := &gemini.Config{
		GeminiBL:          cfg.GeminiBL,
		AuthUser:          cfg.AuthUser,
		XSRFToken:         cfg.XSRFToken,
		DefaultModel:      cfg.DefaultModel,
		LogRequests:       cfg.LogRequests,
		CookieFile:        cfg.CookieFile,
		Proxy:             cfg.Proxy,
		RetryAttempts:     cfg.RetryAttempts,
		RetryDelaySec:     cfg.RetryDelaySec,
		RequestTimeoutSec: cfg.RequestTimeoutSec,
		TemporaryChats:    cfg.TemporaryChats,
	}

	client := gemini.NewClient(geminiCfg)

	// Attempt auto-updating BL from gemini.google.com at startup
	blCtx, blCancel := context.WithTimeout(context.Background(), 10*time.Second)
	if newBL, err := gemini.FetchLatestBL(blCtx, client.HTTPClient()); err == nil && newBL != "" {
		geminiCfg.GeminiBL = newBL
		cfg.GeminiBL = newBL
	}
	blCancel()

	srvCfg := server.NewConfig(cfg.APIKeys, cfg.DefaultModel, cfg.LogRequests)

	uploader := multimodal.NewUploader(
		client.HTTPClient(),
		cookiePath(cfg.CookieFile),
		client.CookieStore(),
	)

	srv := server.New(srvCfg, client, uploader)

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	log.Printf("gomini v%s", "1.1.0")
	log.Printf("  Listening: http://%s", addr)
	log.Printf("  Base URL:  http://localhost:%d/v1", cfg.Port)
	log.Printf("  Cookie:    %s", boolStr(cfg.CookieFile != nil && *cfg.CookieFile != ""))
	log.Printf("  Proxy:     %s", orStr(proxyStr(cfg.Proxy), "none"))
	log.Printf("  BL:        %s", cfg.GeminiBL)
	log.Printf("  Temporary: %s", boolStr(cfg.TemporaryChats))

	httpSrv := &http.Server{
		Addr:    addr,
		Handler: srv,
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-quit
		log.Println("Stopping...")
		if err := httpSrv.Close(); err != nil {
			log.Printf("Error during shutdown: %v", err)
		}
	}()

	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
}

func cookiePath(f *string) string {
	if f != nil {
		return *f
	}
	return ""
}

func proxyStr(f *string) string {
	if f != nil {
		return *f
	}
	return ""
}

func boolStr(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func orStr(s, fallback string) string {
	if s != "" {
		return s
	}
	return fallback
}
