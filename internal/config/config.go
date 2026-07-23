// Package config provides configuration loading from JSON files,
// environment variables, and CLI flags.
package config

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
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
}

func Default() *Config {
	return &Config{
		Port:              8081,
		Host:              "0.0.0.0",
		RetryAttempts:     3,
		RetryDelaySec:     2,
		RequestTimeoutSec: 180,
		GeminiBL:          "boq_assistant-bard-web-server_20260525.09_p0",
		DefaultModel:      "gemini-3.5-flash",
		LogRequests:       true,
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

func (c *Config) ApplyEnv() {
	if v := os.Getenv("GOMINI_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			c.Port = p
		}
	}
	if v := os.Getenv("GOMINI_HOST"); v != "" {
		c.Host = v
	}
	if v := os.Getenv("GOMINI_RETRY_ATTEMPTS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.RetryAttempts = n
		}
	}
	if v := os.Getenv("GOMINI_RETRY_DELAY_SEC"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.RetryDelaySec = n
		}
	}
	if v := os.Getenv("GOMINI_REQUEST_TIMEOUT_SEC"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.RequestTimeoutSec = n
		}
	}
	if v := os.Getenv("GOMINI_GEMINI_BL"); v != "" {
		c.GeminiBL = v
	}
	if v := os.Getenv("GOMINI_AUTH_USER"); v != "" {
		c.AuthUser = &v
	}
	if v := os.Getenv("GOMINI_XSRF_TOKEN"); v != "" {
		c.XSRFToken = &v
	}
	if v := os.Getenv("GOMINI_DEFAULT_MODEL"); v != "" {
		c.DefaultModel = v
	}
	if v := os.Getenv("GOMINI_LOG_REQUESTS"); v != "" {
		c.LogRequests = v == "1" || v == "true"
	}
	if v := os.Getenv("GOMINI_COOKIE_FILE"); v != "" {
		c.CookieFile = &v
	}
	if v := os.Getenv("GOMINI_PROXY"); v != "" {
		c.Proxy = &v
	}
	if v := os.Getenv("GOMINI_API_KEYS"); v != "" {
		c.APIKeys = strings.Split(v, ",")
	}
}

func (c *Config) ApplyFlags() {
	port := flag.Int("port", c.Port, "HTTP listen port")
	host := flag.String("host", c.Host, "HTTP listen address")
	retry := flag.Int("retry-attempts", c.RetryAttempts, "number of retries for upstream requests")
	retryDelay := flag.Int("retry-delay", c.RetryDelaySec, "delay between retries in seconds")
	timeout := flag.Int("request-timeout", c.RequestTimeoutSec, "upstream request timeout in seconds")
	geminiBL := flag.String("gemini-bl", c.GeminiBL, "Gemini build label (bl parameter)")
	defaultModel := flag.String("default-model", c.DefaultModel, "default model name")
	logReqs := flag.Bool("log-requests", c.LogRequests, "enable request logging")
	cookieFile := flag.String("cookie-file", "", "path to cookie file")
	proxy := flag.String("proxy", "", "HTTP proxy URL")
	authUser := flag.String("auth-user", "", "Google auth user index")
	xsrfToken := flag.String("xsrf-token", "", "XSRF token")
	configPath := flag.String("config", "", "path to config file")
	flag.Parse()

	c.Port = *port
	c.Host = *host
	c.RetryAttempts = *retry
	c.RetryDelaySec = *retryDelay
	c.RequestTimeoutSec = *timeout
	c.GeminiBL = *geminiBL
	c.DefaultModel = *defaultModel
	c.LogRequests = *logReqs
	if *cookieFile != "" {
		c.CookieFile = cookieFile
	}
	if *proxy != "" {
		c.Proxy = proxy
	}
	if *authUser != "" {
		c.AuthUser = authUser
	}
	if *xsrfToken != "" {
		c.XSRFToken = xsrfToken
	}
	c.loadExtraConfig(*configPath)
}

func (c *Config) loadExtraConfig(path string) {
	if path == "" {
		path = os.Getenv("GOMINI_CONFIG")
	}
	if path == "" {
		if _, err := os.Stat("config.json"); err == nil {
			path = "config.json"
		}
	}
	if path == "" {
		return
	}
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_ = json.NewDecoder(f).Decode(c)
}
