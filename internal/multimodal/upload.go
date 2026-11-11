// Package multimodal implements Google's Scotty resumable upload
// protocol for image attachment in Gemini requests.
package multimodal

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
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

// DetectImageMIME infers a common raster image MIME type from its file signature.
func DetectImageMIME(imageBytes []byte, fallback string) string {
	if len(imageBytes) >= 8 && string(imageBytes[:8]) == "\x89PNG\r\n\x1a\n" {
		return "image/png"
	}
	if len(imageBytes) >= 3 && imageBytes[0] == 0xff && imageBytes[1] == 0xd8 && imageBytes[2] == 0xff {
		return "image/jpeg"
	}
	if len(imageBytes) >= 6 && (string(imageBytes[:6]) == "GIF87a" || string(imageBytes[:6]) == "GIF89a") {
		return "image/gif"
	}
	if len(imageBytes) >= 12 && string(imageBytes[:4]) == "RIFF" && string(imageBytes[8:12]) == "WEBP" {
		return "image/webp"
	}
	if len(imageBytes) >= 2 && string(imageBytes[:2]) == "BM" {
		return "image/bmp"
	}
	if len(imageBytes) >= 4 && (string(imageBytes[:4]) == "II*\x00" || string(imageBytes[:4]) == "MM\x00*") {
		return "image/tiff"
	}
	if len(imageBytes) >= 12 && string(imageBytes[4:8]) == "ftyp" {
		brand := string(imageBytes[8:12])
		if brand == "avif" || brand == "avis" {
			return "image/avif"
		}
		if brand == "heic" || brand == "heix" || brand == "hevc" || brand == "hevx" {
			return "image/heic"
		}
	}
	if fallback != "" {
		return fallback
	}
	return "image/png"
}

// FetchImageBytes fetches an image from an HTTP(S) URL.
func FetchImageBytes(ctx context.Context, client *http.Client, urlStr string) ([]byte, error) {
	parsed, err := url.Parse(urlStr)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("unsupported image URL scheme: %s", urlStr)
	}
	req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch image status: %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func (u *Uploader) FetchImageBytes(ctx context.Context, urlStr string) ([]byte, error) {
	return FetchImageBytes(ctx, u.client, urlStr)
}

func (u *Uploader) UploadImage(ctx context.Context, imageBytes []byte, mimeType string) (string, error) {
	if len(imageBytes) == 0 {
		return "", fmt.Errorf("empty image bytes")
	}
	mimeType = DetectImageMIME(imageBytes, mimeType)

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

	uploadReq, err := http.NewRequestWithContext(ctx, "POST", uploadURL, bytes.NewReader(imageBytes))
	if err != nil {
		return "", fmt.Errorf("upload: create upload request: %w", err)
	}
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
