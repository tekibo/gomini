// Package multimodal implements Google's Scotty resumable upload
// protocol for image attachment in Gemini requests.
package multimodal

import (
	"context"
	"crypto/sha1"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

const defaultPushID = "feeds/mcudyrk2a4khkz"
const defaultPctx = "CgcSBWjK7pYx"

var rePushID = regexp.MustCompile(`"qKIAYe":"([^"]+)"`)
var rePctx = regexp.MustCompile(`"Ylro7b":"([^"]+)"`)
var reAt = regexp.MustCompile(`"thykhd":"([^"]+)"`)

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

type CookieLoader interface {
	Load(path string) (cookie, sapisid string)
}

type Uploader struct {
	client     *http.Client
	cookiePath string
	cookies    CookieLoader
}

func NewUploader(client *http.Client, cookiePath string, cookies CookieLoader) *Uploader {
	return &Uploader{
		client:     client,
		cookiePath: cookiePath,
		cookies:    cookies,
	}
}

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

func (u *Uploader) UploadImage(ctx context.Context, imageBytes []byte, mimeType string) (string, error) {
	tokens := u.getPageTokens(ctx)
	cookieStr, sapisid := u.cookies.Load(u.cookiePath)

	startURL := "https://content-push.googleapis.com/upload/"

	startReq, err := http.NewRequestWithContext(ctx, "POST", startURL, strings.NewReader(""))
	if err != nil {
		return "", fmt.Errorf("upload: create start request: %w", err)
	}

	startReq.Header.Set("Push-ID", tokens.PushID)
	startReq.Header.Set("X-Tenant-Id", "bard-storage")
	startReq.Header.Set("X-Client-Pctx", tokens.Pctx)
	startReq.Header.Set("X-Goog-Upload-Header-Content-Length", fmt.Sprintf("%d", len(imageBytes)))
	startReq.Header.Set("X-Goog-Upload-Header-Content-Type", mimeType)
	startReq.Header.Set("X-Goog-Upload-Protocol", "resumable")
	startReq.Header.Set("X-Goog-Upload-Command", "start")
	startReq.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=utf-8")
	startReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	if cookieStr != "" {
		startReq.Header.Set("Cookie", cookieStr)
	}
	if sapisid != "" {
		startReq.Header.Set("Authorization", MakeSapisidHash(sapisid))
	}

	startResp, err := u.client.Do(startReq)
	if err != nil {
		return "", fmt.Errorf("upload: start request: %w", err)
	}
	_ = startResp.Body.Close()

	uploadURL := startResp.Header.Get("X-Goog-Upload-URL")
	if uploadURL == "" {
		return "", fmt.Errorf("upload: no upload URL in response headers")
	}

	uploadReq, err := http.NewRequestWithContext(ctx, "POST", uploadURL, nil)
	if err != nil {
		return "", fmt.Errorf("upload: create upload request: %w", err)
	}
	uploadReq.Body = io.NopCloser(strings.NewReader(string(imageBytes)))
	uploadReq.ContentLength = int64(len(imageBytes))
	uploadReq.Header.Set("X-Goog-Upload-Command", "upload, finalize")
	uploadReq.Header.Set("X-Goog-Upload-Offset", "0")
	uploadReq.Header.Set("Content-Type", "application/octet-stream")
	uploadReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	uploadResp, err := u.client.Do(uploadReq)
	if err != nil {
		return "", fmt.Errorf("upload: upload request: %w", err)
	}
	defer func() { _ = uploadResp.Body.Close() }()

	refBytes, err := io.ReadAll(uploadResp.Body)
	if err != nil {
		return "", fmt.Errorf("upload: read response: %w", err)
	}

	fileRef := strings.TrimSpace(string(refBytes))
	if fileRef == "" || fileRef[0] != '/' {
		return "", fmt.Errorf("upload: invalid file reference: %s", fileRef)
	}

	return fileRef, nil
}

func MakeSapisidHash(sapisid string) string {
	h := sha1.New()
	ts := time.Now().Unix()
	_, _ = fmt.Fprintf(h, "%d %s https://gemini.google.com", ts, sapisid)
	return fmt.Sprintf("SAPISIDHASH %d_%x", ts, h.Sum(nil))
}
