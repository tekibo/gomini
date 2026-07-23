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

type Client struct {
	cfg  *Config
	http *http.Client
	cs   *CookieStore
	log  func(string)
}

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
	Port              int
	Host              string
	APIKeys           []string
}

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
		log: func(msg string) {},
	}
}

func (c *Client) SetLogger(logFn func(string)) {
	c.log = logFn
}

type StreamEvent struct {
	Delta string
	Err   error
}

func (c *Client) Generate(ctx context.Context, prompt string, modelID, thinkMode int, fileRefs []string, extra map[int]int) (string, error) {
	body := c.buildPayload(prompt, modelID, thinkMode, fileRefs, extra)
	reqURL := c.buildURL()

	var lastErr error
	for attempt := 0; attempt < c.cfg.RetryAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(time.Duration(c.cfg.RetryDelaySec) * time.Second):
			}
		}

		text, err := c.doRequest(ctx, reqURL, body)
		if err == nil {
			return text, nil
		}
		lastErr = err
		c.log(fmt.Sprintf("Retry %d/%d: %v", attempt+1, c.cfg.RetryAttempts, err))
	}
	return "", lastErr
}

func (c *Client) GenerateStream(ctx context.Context, prompt string, modelID, thinkMode int, fileRefs []string, extra map[int]int) <-chan StreamEvent {
	ch := make(chan StreamEvent)

	go func() {
		defer close(ch)

		body := c.buildPayload(prompt, modelID, thinkMode, fileRefs, extra)
		reqURL := c.buildURL()

		var lastErr error
		for attempt := 0; attempt < c.cfg.RetryAttempts; attempt++ {
			if attempt > 0 {
				select {
				case <-ctx.Done():
					ch <- StreamEvent{Err: ctx.Err()}
					return
				case <-time.After(time.Duration(c.cfg.RetryDelaySec) * time.Second):
				}
			}

			err := c.doStreamRequest(ctx, reqURL, body, ch)
			if err == nil {
				return
			}
			lastErr = err
			c.log(fmt.Sprintf("Stream retry %d/%d: %v", attempt+1, c.cfg.RetryAttempts, err))
		}
		if lastErr != nil {
			ch <- StreamEvent{Err: lastErr}
		}
	}()

	return ch
}

func (c *Client) doRequest(ctx context.Context, reqURL, body string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", reqURL, strings.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	return extractResponseText(string(raw)), nil
}

func (c *Client) doStreamRequest(ctx context.Context, reqURL, body string, ch chan<- StreamEvent) error {
	req, err := http.NewRequestWithContext(ctx, "POST", reqURL, strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("create stream request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("stream request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	prevText := ""

	for scanner.Scan() {
		line := scanner.Text()
		texts := extractTextsFromLine(line)
		for _, t := range texts {
			if len(t) > len(prevText) {
				delta := cleanText(t[len(prevText):])
				if delta != "" {
					select {
					case ch <- StreamEvent{Delta: delta}:
					case <-ctx.Done():
						return ctx.Err()
					}
				}
				prevText = t
			}
		}
	}

	return scanner.Err()
}

var cleanRE = regexp.MustCompile(
	"```(?:python|javascript|text)\\?code_(?:reference|stdout)&code_event_index=\\d+\\n.*?```\\n?",
)
var cardRE = regexp.MustCompile(`http://googleusercontent\.com/card_content/\d+\n?`)

func cleanText(text string) string {
	text = cleanRE.ReplaceAllString(text, "")
	text = cardRE.ReplaceAllString(text, "")
	return strings.TrimSpace(text)
}

func extractTextsFromLine(line string) []string {
	if !strings.Contains(line, "wrb.fr") || len(line) < 200 {
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
	if len(innerStr) < 50 {
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

func (c *Client) buildPayload(prompt string, modelID, thinkMode int, fileRefs []string, extra map[int]int) string {
	inner := make([]any, 102)

	if len(fileRefs) > 0 {
		refs := make([][]any, len(fileRefs))
		for i, ref := range fileRefs {
			refs[i] = []any{nil, nil, ref}
		}
		inner[0] = []any{prompt, 0, nil, refs, nil, nil, 0}
	} else {
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
	inner[41] = []int{2}
	inner[53] = 0
	inner[59] = uuid()
	inner[61] = []any{}
	inner[68] = 1
	inner[79] = modelID

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

func (c *Client) setHeaders(req *http.Request) {
	prefix := ""
	if c.cfg.AuthUser != nil && *c.cfg.AuthUser != "" {
		prefix = fmt.Sprintf("/u/%s", *c.cfg.AuthUser)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://gemini.google.com")
	req.Header.Set("Referer", fmt.Sprintf("https://gemini.google.com%s/app", prefix))
	req.Header.Set("X-Same-Domain", "1")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	if c.cfg.AuthUser != nil && *c.cfg.AuthUser != "" {
		req.Header.Set("X-Goog-AuthUser", *c.cfg.AuthUser)
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

func (c *Client) HTTPClient() *http.Client { return c.http }
func (c *Client) CookieStore() *CookieStore { return c.cs }
