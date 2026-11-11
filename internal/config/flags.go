package config

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
)

// ApplyFlags parses command-line flags from os.Args and overrides configuration values.
func (c *Config) ApplyFlags() {
	if err := c.ApplyArgs(os.Args[1:]); err != nil {
		if err == flag.ErrHelp {
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "config: parse flags: %v\n", err)
		os.Exit(2)
	}
}

// ApplyArgs parses flags from an argument slice and overrides configuration values.
// Configuration precedence order is: Defaults < Config File < Environment < Flags.
func (c *Config) ApplyArgs(args []string) error {
	path := findConfigPath(args)
	if path != "" {
		c.loadExtraConfig(path)
		c.ApplyEnv()
	}

	fs := flag.NewFlagSet("gomini", flag.ContinueOnError)
	port := fs.Int("port", c.Port, "HTTP listen port")
	host := fs.String("host", c.Host, "HTTP listen address")
	retry := fs.Int("retry-attempts", c.RetryAttempts, "number of retries for upstream requests")
	retryDelay := fs.Int("retry-delay", c.RetryDelaySec, "delay between retries in seconds")
	timeout := fs.Int("request-timeout", c.RequestTimeoutSec, "upstream request timeout in seconds")
	geminiBL := fs.String("gemini-bl", c.GeminiBL, "Gemini build label (bl parameter)")
	defaultModel := fs.String("default-model", c.DefaultModel, "default model name")
	logReqs := fs.Bool("log-requests", c.LogRequests, "enable request logging")
	cookieFile := fs.String("cookie-file", "", "path to cookie file")
	proxy := fs.String("proxy", "", "HTTP proxy URL")
	authUser := fs.String("auth-user", "", "Google auth user index")
	xsrfToken := fs.String("xsrf-token", "", "XSRF token")
	tempChats := fs.Bool("temporary-chats", c.TemporaryChats, "use temporary chats (do not persist history)")
	sendTicket := fs.Bool("send-model-ticket", c.SendModelTicket, "send model ticket header")
	ticketTimeout := fs.Int("ticket-timeout", c.TicketTimeoutSec, "timeout in seconds for model ticket requests")
	idleTimeout := fs.Int("idle-timeout", c.IdleTimeoutSec, "idle timeout in seconds for upstream connections")
	_ = fs.String("config", "", "path to config file")

	if err := fs.Parse(args); err != nil {
		return err
	}

	c.Port = *port
	c.Host = *host
	c.RetryAttempts = *retry
	c.RetryDelaySec = *retryDelay
	c.RequestTimeoutSec = *timeout
	c.TicketTimeoutSec = *ticketTimeout
	c.IdleTimeoutSec = *idleTimeout
	c.GeminiBL = *geminiBL
	c.DefaultModel = *defaultModel
	c.LogRequests = *logReqs
	c.TemporaryChats = *tempChats
	c.SendModelTicket = *sendTicket
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
	return nil
}

func findConfigPath(args []string) string {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if (arg == "-config" || arg == "--config") && i+1 < len(args) {
			return args[i+1]
		}
		if strings.HasPrefix(arg, "-config=") {
			return strings.TrimPrefix(arg, "-config=")
		}
		if strings.HasPrefix(arg, "--config=") {
			return strings.TrimPrefix(arg, "--config=")
		}
	}
	if env := os.Getenv("GOMINI_CONFIG"); env != "" {
		return env
	}
	if _, err := os.Stat("config.json"); err == nil {
		return "config.json"
	}
	return ""
}

func (c *Config) loadExtraConfig(path string) {
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
