package config

import (
	"encoding/json"
	"flag"
	"os"
)

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
	tempChats := flag.Bool("temporary-chats", c.TemporaryChats, "use temporary chats (do not persist history)")
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
	c.TemporaryChats = *tempChats
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
