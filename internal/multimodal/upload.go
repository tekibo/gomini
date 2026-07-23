// Package multimodal implements Google's Scotty resumable upload
// protocol for image attachment in Gemini requests.
package multimodal

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

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
