package multimodal

import (
	"context"
	"crypto/sha1"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sync"
	"time"
)

const defaultPushID = "feeds/mcudyrk2a4khkz"
const defaultPctx = "CgcSBWjK7pYx"

var rePushID = regexp.MustCompile(`"qKIAYe":"([^"]+)"`)
var rePctx = regexp.MustCompile(`"Ylro7b":"([^"]+)"`)
var reAt = regexp.MustCompile(`"thykhd":"([^"]+)"`)

// PageTokens represents push and context tokens retrieved from the Gemini web page.
type PageTokens struct {
	PushID string
	Pctx   string
	At     string
}

type tokenCache struct {
	mu     sync.Mutex
	tokens PageTokens
	ts     time.Time
}

var tokCache tokenCache

func (u *Uploader) getPageTokens(ctx context.Context) PageTokens {
	tokCache.mu.Lock()
	if time.Since(tokCache.ts) < 10*time.Minute && tokCache.tokens.PushID != "" {
		t := tokCache.tokens
		tokCache.mu.Unlock()
		return t
	}
	tokCache.mu.Unlock()

	req, err := http.NewRequestWithContext(ctx, "GET", "https://gemini.google.com/app", nil)
	if err != nil {
		return PageTokens{PushID: defaultPushID, Pctx: defaultPctx}
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	if cookieStr, _ := u.cookies.Load(u.cookiePath); cookieStr != "" {
		req.Header.Set("Cookie", cookieStr)
	}

	resp, err := u.client.Do(req)
	if err != nil {
		return PageTokens{PushID: defaultPushID, Pctx: defaultPctx}
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return PageTokens{PushID: defaultPushID, Pctx: defaultPctx}
	}

	html := string(body)
	tokens := PageTokens{}
	if m := rePushID.FindStringSubmatch(html); len(m) > 1 {
		tokens.PushID = m[1]
	} else {
		tokens.PushID = defaultPushID
	}
	if m := rePctx.FindStringSubmatch(html); len(m) > 1 {
		tokens.Pctx = m[1]
	} else {
		tokens.Pctx = defaultPctx
	}
	if m := reAt.FindStringSubmatch(html); len(m) > 1 {
		tokens.At = m[1]
	}

	tokCache.mu.Lock()
	tokCache.tokens = tokens
	tokCache.ts = time.Now()
	tokCache.mu.Unlock()

	return tokens
}

// MakeSapisidHash calculates the SAPISIDHASH header value for Google authentication.
func MakeSapisidHash(sapisid string) string {
	h := sha1.New()
	ts := time.Now().Unix()
	_, _ = fmt.Fprintf(h, "%d %s https://gemini.google.com", ts, sapisid)
	return fmt.Sprintf("SAPISIDHASH %d_%x", ts, h.Sum(nil))
}
