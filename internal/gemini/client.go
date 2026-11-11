package gemini

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Client communicates with the Gemini StreamGenerate backend.
type Client struct {
	cfg  *Config
	http *http.Client
	cs   *CookieStore
	log  func(string)
}

// Config defines client options and network timeouts.
type Config struct {
	GeminiBL          string
	AuthUser          *string
	XSRFToken         *string
	DefaultModel      string
	LogRequests       bool
	CookieFile        *string
	Proxy             *string
	RetryAttempts     int
	RetryDelaySec     int
	RequestTimeoutSec int
	TicketTimeoutSec  int
	IdleTimeoutSec    int
	Port              int
	Host              string
	APIKeys           []string
	TemporaryChats    bool
	SendModelTicket   bool
	ModelTickets      map[string]string
}

var reBL = regexp.MustCompile(`(boq_assistant-bard-web-server_\d+\.\d+_p\d+)`)

// FetchLatestBL attempts to scrape the latest gemini_bl from gemini.google.com/app.
func FetchLatestBL(ctx context.Context, client *http.Client) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://gemini.google.com/app", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	m := reBL.FindStringSubmatch(string(body))
	if len(m) > 1 {
		return m[1], nil
	}
	return "", fmt.Errorf("gemini_bl not found on page")
}

// UpdateBLIfNeeded checks and refreshes gemini_bl if changed.
func (c *Client) UpdateBLIfNeeded(ctx context.Context) bool {
	newBL, err := FetchLatestBL(ctx, c.http)
	if err == nil && newBL != "" && newBL != c.cfg.GeminiBL {
		c.log(fmt.Sprintf("BL auto-updated: %s -> %s", c.cfg.GeminiBL, newBL))
		c.cfg.GeminiBL = newBL
		return true
	}
	return false
}

// RefreshBLAndXSRF fetches the Gemini app page to refresh both gemini_bl and the xsrf token (SNlM0e).
func (c *Client) RefreshBLAndXSRF(ctx context.Context) bool {
	cookiePath := ""
	if c.cfg.CookieFile != nil {
		cookiePath = *c.cfg.CookieFile
	}
	newBL, newXSRF, err := RefreshBLAndXSRF(ctx, c.http, c.cs, cookiePath, c.cfg.AuthUser)
	if err != nil {
		c.log(fmt.Sprintf("BL/XSRF refresh failed: %v", err))
		return false
	}
	changed := false
	if newBL != "" && newBL != c.cfg.GeminiBL {
		c.log(fmt.Sprintf("BL auto-updated: %s -> %s", c.cfg.GeminiBL, newBL))
		c.cfg.GeminiBL = newBL
		changed = true
	}
	if newXSRF != "" && (c.cfg.XSRFToken == nil || *c.cfg.XSRFToken != newXSRF) {
		c.log(fmt.Sprintf("XSRF token auto-updated (len %d)", len(newXSRF)))
		c.cfg.XSRFToken = &newXSRF
		changed = true
	}
	return changed
}

// NewClient creates and initializes a new Gemini client.
func NewClient(cfg *Config) *Client {
	transport := &http.Transport{}
	if cfg.Proxy != nil && *cfg.Proxy != "" {
		proxyURL, err := url.Parse(*cfg.Proxy)
		if err == nil {
			transport.Proxy = http.ProxyURL(proxyURL)
		}
	}

	return &Client{
		cfg: cfg,
		http: &http.Client{
			Transport: transport,
			Timeout:   time.Duration(cfg.RequestTimeoutSec) * time.Second,
		},
		cs:  &CookieStore{},
		log: func(_ string) {},
	}
}

// SetLogger configures a logging function for client operations.
func (c *Client) SetLogger(logFn func(string)) {
	c.log = logFn
}

// StreamEvent represents a text delta or error emitted during streaming generation.
type StreamEvent struct {
	Delta string
	Err   error
}

// Generate sends a non-streaming request to Gemini and returns the extracted reply text.
func (c *Client) Generate(ctx context.Context, prompt string, modelID, thinkMode int, fileRefs []string, extra map[int]int, ticketHeader ...string) (string, error) {
	ticketHdr := ""
	if len(ticketHeader) > 0 {
		ticketHdr = ticketHeader[0]
	}

	var lastErr error
	attempts := c.cfg.RetryAttempts
	if attempts < 1 {
		attempts = 3
	}

	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(time.Duration(c.cfg.RetryDelaySec) * time.Second):
			}
		}

		reqUUID := uuid()
		body := c.buildPayload(prompt, modelID, thinkMode, fileRefs, extra, reqUUID)
		reqURL := c.buildURL()

		reqCtx := ctx
		if ticketHdr != "" && c.cfg.TicketTimeoutSec > 0 {
			var cancel context.CancelFunc
			reqCtx, cancel = context.WithTimeout(ctx, time.Duration(c.cfg.TicketTimeoutSec)*time.Second)
			defer cancel()
		}

		text, err := c.doRequest(reqCtx, reqURL, body, ticketHdr, reqUUID, len(fileRefs) > 0)
		if err == nil {
			return text, nil
		}

		if ticketHdr != "" {
			c.log(fmt.Sprintf("Model ticket request failed (%v); falling back without ticket header", err))
			ticketHdr = ""
			continue
		}

		if (strings.Contains(err.Error(), "HTTP 405") || strings.Contains(err.Error(), "HTTP 400")) && c.RefreshBLAndXSRF(ctx) {
			c.log("Retrying with refreshed BL/XSRF...")
			continue
		}

		lastErr = err
		c.log(fmt.Sprintf("Retry %d/%d: %v", attempt+1, attempts, err))
	}
	return "", lastErr
}

// GenerateStream initiates streaming generation, emitting text chunks over the returned channel.
func (c *Client) GenerateStream(ctx context.Context, prompt string, modelID, thinkMode int, fileRefs []string, extra map[int]int, ticketHeader ...string) <-chan StreamEvent {
	ch := make(chan StreamEvent)

	ticketHdr := ""
	if len(ticketHeader) > 0 {
		ticketHdr = ticketHeader[0]
	}

	go func() {
		defer close(ch)

		emittedRawText := ""
		var lastErr error
		attempts := c.cfg.RetryAttempts
		if attempts < 1 {
			attempts = 3
		}

		for attempt := 0; attempt < attempts; attempt++ {
			if attempt > 0 {
				select {
				case <-ctx.Done():
					ch <- StreamEvent{Err: ctx.Err()}
					return
				case <-time.After(time.Duration(c.cfg.RetryDelaySec) * time.Second):
				}
			}

			reqUUID := uuid()
			body := c.buildPayload(prompt, modelID, thinkMode, fileRefs, extra, reqUUID)
			reqURL := c.buildURL()

			reqCtx := ctx
			if ticketHdr != "" && c.cfg.TicketTimeoutSec > 0 && emittedRawText == "" {
				var cancel context.CancelFunc
				reqCtx, cancel = context.WithTimeout(ctx, time.Duration(c.cfg.TicketTimeoutSec)*time.Second)
				defer cancel()
			}

			err := c.doStreamRequest(reqCtx, reqURL, body, ticketHdr, reqUUID, len(fileRefs) > 0, &emittedRawText, ch)
			if err == nil {
				return
			}

			if emittedRawText != "" {
				c.log(fmt.Sprintf("Stream interrupted after partial output (%d chars), ending cleanly: %v", len(emittedRawText), err))
				return
			}

			if ticketHdr != "" {
				c.log(fmt.Sprintf("Stream model ticket failed (%v); falling back without ticket header", err))
				ticketHdr = ""
				continue
			}

			if (strings.Contains(err.Error(), "HTTP 405") || strings.Contains(err.Error(), "HTTP 400")) && c.RefreshBLAndXSRF(ctx) {
				c.log("Stream retrying with refreshed BL/XSRF...")
				continue
			}

			lastErr = err
			c.log(fmt.Sprintf("Stream retry %d/%d: %v", attempt+1, attempts, err))
		}
		if lastErr != nil {
			ch <- StreamEvent{Err: lastErr}
		}
	}()

	return ch
}

func (c *Client) doRequest(ctx context.Context, reqURL, body, ticketHdr, reqUUID string, hasFileRefs bool) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", reqURL, strings.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	c.setHeaders(req, ticketHdr, reqUUID, hasFileRefs)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("gemini upstream returned HTTP %d", resp.StatusCode)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	rawStr := string(raw)
	if match := bardErrorRE.FindStringSubmatch(rawStr); match != nil {
		return "", formatBardError(match[1])
	}

	return extractResponseText(rawStr), nil
}

func (c *Client) doStreamRequest(ctx context.Context, reqURL, body, ticketHdr, reqUUID string, hasFileRefs bool, emittedRawText *string, ch chan<- StreamEvent) error {
	req, err := http.NewRequestWithContext(ctx, "POST", reqURL, strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("create stream request: %w", err)
	}
	c.setHeaders(req, ticketHdr, reqUUID, hasFileRefs)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("stream request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("gemini upstream returned HTTP %d", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()

		if match := bardErrorRE.FindStringSubmatch(line); match != nil {
			return formatBardError(match[1])
		}

		texts := extractTextsFromLine(line)
		for _, t := range texts {
			if t == *emittedRawText || strings.HasPrefix(*emittedRawText, t) {
				continue
			}
			if !strings.HasPrefix(t, *emittedRawText) {
				return fmt.Errorf("gemini stream content changed during retry")
			}
			delta := cleanText(t[len(*emittedRawText):], false)
			*emittedRawText = t
			if delta != "" {
				select {
				case ch <- StreamEvent{Delta: delta}:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}
	}

	return scanner.Err()
}

var cleanRE = regexp.MustCompile(
	"```(?:python|javascript|text)\\?code_(?:reference|stdout)&code_event_index=\\d+\\n.*?```\\n?",
)
var cardRE = regexp.MustCompile(`http://googleusercontent\.com/card_content/\d+\n?`)
var bardErrorRE = regexp.MustCompile(`BardErrorInfo"?,?\s*\[(\d+)\]`)

func formatBardError(code string) error {
	hint := ""
	switch code {
	case "1060":
		hint = ": IP temporarily blocked"
	case "1037":
		hint = ": usage limit exceeded"
	case "1013":
		hint = ": transient upstream error"
	case "1003":
		hint = ": file not bound to conversation"
	case "1185":
		hint = ": request rejected"
	}
	return fmt.Errorf("gemini upstream error [%s]%s", code, hint)
}

func cleanText(text string, strip ...bool) string {
	doStrip := true
	if len(strip) > 0 {
		doStrip = strip[0]
	}
	text = cleanRE.ReplaceAllString(text, "")
	text = cardRE.ReplaceAllString(text, "")
	if doStrip {
		text = strings.TrimSpace(text)
	}
	return text
}

func extractTextsFromLine(line string) []string {
	if !strings.Contains(line, "wrb.fr") {
		return nil
	}

	var arr []json.RawMessage
	if err := json.Unmarshal([]byte(line), &arr); err != nil {
		return nil
	}
	if len(arr) < 1 {
		return nil
	}

	var first []json.RawMessage
	if err := json.Unmarshal(arr[0], &first); err != nil {
		return nil
	}
	if len(first) < 3 {
		return nil
	}

	var innerStr string
	if err := json.Unmarshal(first[2], &innerStr); err != nil {
		return nil
	}

	var inner []json.RawMessage
	if err := json.Unmarshal([]byte(innerStr), &inner); err != nil {
		return nil
	}
	if len(inner) < 5 {
		return nil
	}

	var parts []json.RawMessage
	if err := json.Unmarshal(inner[4], &parts); err != nil {
		return nil
	}

	var texts []string
	for _, part := range parts {
		var partArr []json.RawMessage
		if err := json.Unmarshal(part, &partArr); err != nil {
			continue
		}
		if len(partArr) < 2 {
			continue
		}
		var strList []string
		if err := json.Unmarshal(partArr[1], &strList); err != nil {
			continue
		}
		for _, s := range strList {
			if s != "" {
				texts = append(texts, s)
			}
		}
	}
	return texts
}

func extractResponseText(raw string) string {
	lastText := ""
	for line := range strings.SplitSeq(raw, "\n") {
		for _, t := range extractTextsFromLine(line) {
			if len(t) > len(lastText) {
				lastText = t
			}
		}
	}
	return cleanText(lastText)
}

func (c *Client) buildPayload(prompt string, modelID, thinkMode int, fileRefs []string, extra map[int]int, reqUUID string) string {
	var inner []any
	if len(fileRefs) > 0 {
		inner = make([]any, 81)
		refs := make([][]any, len(fileRefs))
		for i, ref := range fileRefs {
			refs[i] = []any{[]string{ref}, "image.png"}
		}
		inner[0] = []any{prompt, 0, nil, refs, nil, nil, 0}
	} else {
		inner = make([]any, 102)
		inner[0] = []any{prompt, 0, nil, nil, nil, nil, 0}
	}

	inner[1] = []string{"en"}
	inner[2] = []any{"", "", "", nil, nil, nil, nil, nil, nil, ""}
	inner[6] = []int{0}
	inner[7] = 1
	inner[10] = 1
	inner[11] = 0
	inner[17] = [][]int{{thinkMode}}
	inner[18] = 0
	inner[27] = 1
	inner[30] = []int{4}
	if c.cfg.TemporaryChats {
		inner[41] = []int{1}
		inner[45] = 1
	} else {
		inner[41] = []int{2}
	}
	inner[53] = 0
	inner[59] = reqUUID
	inner[61] = []any{}
	inner[68] = 1
	inner[79] = modelID
	if len(fileRefs) > 0 {
		inner[80] = 1
	}

	for k, v := range extra {
		inner[k] = v
	}

	innerJSON, _ := json.Marshal(inner)
	outer := []any{nil, string(innerJSON)}
	freq, _ := json.Marshal(outer)

	params := url.Values{}
	params.Set("f.req", string(freq))
	if c.cfg.XSRFToken != nil && *c.cfg.XSRFToken != "" {
		params.Set("at", *c.cfg.XSRFToken)
	}
	return params.Encode()
}

func (c *Client) buildURL() string {
	reqID := time.Now().Unix() % 1000000
	prefix := ""
	if c.cfg.AuthUser != nil && *c.cfg.AuthUser != "" {
		prefix = fmt.Sprintf("/u/%s", *c.cfg.AuthUser)
	}
	return fmt.Sprintf(
		"https://gemini.google.com%s/_/BardChatUi/data/assistant.lamda.BardFrontendService/StreamGenerate?bl=%s&hl=en&_reqid=%d&rt=c",
		prefix, c.cfg.GeminiBL, reqID,
	)
}

func (c *Client) setHeaders(req *http.Request, ticketHdr, reqUUID string, hasFileRefs bool) {
	prefix := ""
	if c.cfg.AuthUser != nil && *c.cfg.AuthUser != "" {
		prefix = fmt.Sprintf("/u/%s", *c.cfg.AuthUser)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://gemini.google.com")
	req.Header.Set("Referer", fmt.Sprintf("https://gemini.google.com%s/app", prefix))
	req.Header.Set("X-Same-Domain", "1")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	req.Header.Set("Connection", "close")

	if c.cfg.AuthUser != nil && *c.cfg.AuthUser != "" {
		req.Header.Set("X-Goog-AuthUser", *c.cfg.AuthUser)
	}
	if ticketHdr != "" && c.cfg.SendModelTicket {
		req.Header.Set("X-Goog-Ext-525001261-Jspb", ticketHdr)
	}
	if hasFileRefs && reqUUID != "" {
		req.Header.Set("x-goog-ext-525005358-jspb", fmt.Sprintf(`["%s",1]`, reqUUID))
	}

	cookiePath := ""
	if c.cfg.CookieFile != nil {
		cookiePath = *c.cfg.CookieFile
	}
	cookieStr, sapisid := c.cs.Load(cookiePath)
	if cookieStr != "" {
		req.Header.Set("Cookie", cookieStr)
	}
	if sapisid != "" {
		req.Header.Set("Authorization", MakeSapisidHash(sapisid))
	}
}

func uuid() string {
	b := make([]byte, 16)
	rand.Read(b)
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// HTTPClient returns the underlying *http.Client.
func (c *Client) HTTPClient() *http.Client { return c.http }

// CookieStore returns the client's cookie store.
func (c *Client) CookieStore() *CookieStore { return c.cs }
