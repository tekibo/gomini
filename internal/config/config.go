// Package config provides configuration loading from JSON files,
// environment variables, and CLI flags.
package config

import (
	"encoding/json"
	"fmt"
	"os"
)

type Config struct {
	Port              int      `json:"port"`
	Host              string   `json:"host"`
	RetryAttempts     int      `json:"retry_attempts"`
	RetryDelaySec     int      `json:"retry_delay_sec"`
	RequestTimeoutSec int      `json:"request_timeout_sec"`
	GeminiBL          string   `json:"gemini_bl"`
	AuthUser          *string  `json:"auth_user"`
	XSRFToken         *string  `json:"xsrf_token"`
	DefaultModel      string   `json:"default_model"`
	LogRequests       bool     `json:"log_requests"`
	CookieFile        *string  `json:"cookie_file"`
	Proxy             *string  `json:"proxy"`
	APIKeys           []string `json:"api_keys"`
	TemporaryChats    bool     `json:"temporary_chats"`
}

func Default() *Config {
	return &Config{
		Port:              8081,
		Host:              "0.0.0.0",
		RetryAttempts:     3,
		RetryDelaySec:     2,
		RequestTimeoutSec: 180,
		GeminiBL:          "boq_assistant-bard-web-server_20260723.00_p0",
		DefaultModel:      "gemini-3.6-flash",
		LogRequests:       true,
		TemporaryChats:    false,
	}
}

func LoadFile(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("config: open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	cfg := Default()
	if err := json.NewDecoder(f).Decode(cfg); err != nil {
		return nil, fmt.Errorf("config: decode %s: %w", path, err)
	}
	return cfg, nil
}
