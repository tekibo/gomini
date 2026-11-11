// Package multimodal implements Google's Scotty resumable upload
// protocol for image attachment in Gemini requests.
package multimodal

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"
)

const (
	maxRemoteImageBytes     = 10 * 1024 * 1024 // 10 MiB limit
	maxRemoteImageRedirects = 3
)

var blockedNets = []*net.IPNet{
	mustParseCIDR("0.0.0.0/8"),
	mustParseCIDR("100.64.0.0/10"),      // RFC 6598 CGNAT
	mustParseCIDR("192.0.0.0/24"),       // RFC 6890
	mustParseCIDR("192.0.2.0/24"),       // RFC 5737 TEST-NET-1
	mustParseCIDR("198.18.0.0/15"),      // RFC 2544 Benchmark
	mustParseCIDR("198.51.100.0/24"),    // RFC 5737 TEST-NET-2
	mustParseCIDR("203.0.113.0/24"),     // RFC 5737 TEST-NET-3
	mustParseCIDR("240.0.0.0/4"),        // RFC 1112 Reserved
	mustParseCIDR("255.255.255.255/32"), // Broadcast
	mustParseCIDR("2001:db8::/32"),      // RFC 3849 Documentation
}

func mustParseCIDR(s string) *net.IPNet {
	_, ipnet, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	return ipnet
}

// IsPublicIP reports whether ip is a publicly routable global unicast IP address.
func IsPublicIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ipv4 := ip.To4(); ipv4 != nil {
		ip = ipv4
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return false
	}
	for _, blocked := range blockedNets {
		if blocked.Contains(ip) {
			return false
		}
	}
	return true
}

// ValidateRemoteImageURL validates URL structure and rejects private IP literals and non-standard ports.
func ValidateRemoteImageURL(parsed *url.URL) error {
	if parsed == nil {
		return fmt.Errorf("nil URL")
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return fmt.Errorf("remote image scheme must be http or https, got %q", parsed.Scheme)
	}
	if parsed.User != nil {
		return fmt.Errorf("remote image URL with credentials is not allowed")
	}
	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("remote image URL missing hostname")
	}
	port := parsed.Port()
	if parsed.Scheme == "https" && port != "" && port != "443" {
		return fmt.Errorf("remote image non-standard HTTPS port not allowed: %s", port)
	}
	if parsed.Scheme == "http" && port != "" && port != "80" {
		return fmt.Errorf("remote image non-standard HTTP port not allowed: %s", port)
	}
	if ip := net.ParseIP(host); ip != nil {
		if !IsPublicIP(ip) {
			return fmt.Errorf("remote image host is not a public IP: %s", host)
		}
	}
	return nil
}

// ValidateHostPublic resolves host and ensures all returned IP addresses are public.
func ValidateHostPublic(ctx context.Context, host string) error {
	if ip := net.ParseIP(host); ip != nil {
		if !IsPublicIP(ip) {
			return fmt.Errorf("remote image host is not a public IP: %s", host)
		}
		return nil
	}
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return fmt.Errorf("resolve remote image host %q: %w", host, err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("no IP addresses returned for host %q", host)
	}
	for _, ip := range ips {
		if !IsPublicIP(ip) {
			return fmt.Errorf("remote image host %q resolves to non-public IP %s", host, ip.String())
		}
	}
	return nil
}

func newSafeHTTPClient() *http.Client {
	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
		Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip := net.ParseIP(host)
			if ip != nil && !IsPublicIP(ip) {
				return fmt.Errorf("remote image destination is not a public IP: %s", host)
			}
			return nil
		},
	}

	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          10,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	return &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRemoteImageRedirects {
				return fmt.Errorf("remote image exceeded redirect limit (%d)", maxRemoteImageRedirects)
			}
			if err := ValidateRemoteImageURL(req.URL); err != nil {
				return fmt.Errorf("remote image redirect rejected: %w", err)
			}
			ctx, cancel := context.WithTimeout(req.Context(), 5*time.Second)
			defer cancel()
			if err := ValidateHostPublic(ctx, req.URL.Hostname()); err != nil {
				return fmt.Errorf("remote image redirect rejected: %w", err)
			}
			return nil
		},
	}
}

// CookieLoader loads cookies and SAPISID tokens from a path.
type CookieLoader interface {
	Load(path string) (cookie, sapisid string)
}

// Uploader uploads images to Google's Scotty resumable upload endpoint.
type Uploader struct {
	client     *http.Client
	cookiePath string
	cookies    CookieLoader
}

// NewUploader constructs a new Uploader.
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

// FetchImageBytes fetches an image from an HTTP(S) URL with SSRF protection,
// strict redirect bounds, and size limits. If client is nil, an SSRF-safe HTTP client is used.
func FetchImageBytes(ctx context.Context, client *http.Client, urlStr string) ([]byte, error) {
	if len(urlStr) > 8192 {
		return nil, fmt.Errorf("remote image URL exceeds maximum length of 8192 characters")
	}
	parsed, err := url.Parse(urlStr)
	if err != nil {
		return nil, fmt.Errorf("parse remote image URL: %w", err)
	}
	if err := ValidateRemoteImageURL(parsed); err != nil {
		return nil, err
	}
	if err := ValidateHostPublic(ctx, parsed.Hostname()); err != nil {
		return nil, err
	}

	if client == nil {
		client = newSafeHTTPClient()
	}

	req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	if err != nil {
		return nil, fmt.Errorf("create remote image request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	req.Header.Set("Accept", "image/png,image/jpeg,image/webp,image/gif,image/*;q=0.8")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch remote image: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("remote image fetch failed with status: %d", resp.StatusCode)
	}

	if resp.ContentLength > maxRemoteImageBytes {
		return nil, fmt.Errorf("remote image exceeds size limit of %d bytes", maxRemoteImageBytes)
	}

	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if ct != "" {
		baseType := strings.TrimSpace(strings.Split(ct, ";")[0])
		if !strings.HasPrefix(baseType, "image/") && baseType != "application/octet-stream" {
			return nil, fmt.Errorf("remote image response Content-Type is not an image: %s", baseType)
		}
	}

	limited := io.LimitReader(resp.Body, maxRemoteImageBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read remote image: %w", err)
	}
	if int64(len(data)) > maxRemoteImageBytes {
		return nil, fmt.Errorf("remote image exceeds size limit of %d bytes", maxRemoteImageBytes)
	}

	detected := DetectImageMIME(data, "")
	if detected == "" {
		return nil, fmt.Errorf("remote image payload is not a supported raster image")
	}

	return data, nil
}

// FetchImageBytes fetches an image from an HTTP/HTTPS URL with SSRF protection.
func (u *Uploader) FetchImageBytes(ctx context.Context, urlStr string) ([]byte, error) {
	return FetchImageBytes(ctx, nil, urlStr)
}

// UploadImage performs the 2-step resumable upload to Google Scotty and returns the upload blob path.
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
