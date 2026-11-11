package config

import (
	"os"
	"strconv"
	"strings"
)

// ApplyEnv overrides configuration values from environment variables.
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
	if v := os.Getenv("GOMINI_TEMPORARY_CHATS"); v != "" {
		c.TemporaryChats = v == "1" || v == "true"
	}
	if v := os.Getenv("GOMINI_SEND_MODEL_TICKET"); v != "" {
		c.SendModelTicket = v == "1" || v == "true"
	}
	if v := os.Getenv("GOMINI_TICKET_TIMEOUT_SEC"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.TicketTimeoutSec = n
		}
	}
	if v := os.Getenv("GOMINI_IDLE_TIMEOUT_SEC"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.IdleTimeoutSec = n
		}
	}
}
