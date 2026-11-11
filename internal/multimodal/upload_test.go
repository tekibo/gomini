package multimodal

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"testing"
)

func TestDetectImageMIME(t *testing.T) {
	pngHeader := []byte("\x89PNG\r\n\x1a\nfake png data")
	if mime := DetectImageMIME(pngHeader, ""); mime != "image/png" {
		t.Errorf("expected image/png, got %s", mime)
	}

	jpegHeader := []byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10}
	if mime := DetectImageMIME(jpegHeader, ""); mime != "image/jpeg" {
		t.Errorf("expected image/jpeg, got %s", mime)
	}

	gifHeader := []byte("GIF89a...")
	if mime := DetectImageMIME(gifHeader, ""); mime != "image/gif" {
		t.Errorf("expected image/gif, got %s", mime)
	}

	webpHeader := []byte("RIFF1234WEBP...")
	if mime := DetectImageMIME(webpHeader, ""); mime != "image/webp" {
		t.Errorf("expected image/webp, got %s", mime)
	}

	fallback := []byte("unrecognized data")
	if mime := DetectImageMIME(fallback, "image/custom"); mime != "image/custom" {
		t.Errorf("expected image/custom fallback, got %s", mime)
	}
}

func TestFetchImageBytesInvalidScheme(t *testing.T) {
	client := &http.Client{}
	_, err := FetchImageBytes(context.Background(), client, "file:///etc/passwd")
	if err == nil {
		t.Errorf("expected error for file:// scheme")
	}

	_, err = FetchImageBytes(context.Background(), client, "ftp://example.com/image.png")
	if err == nil {
		t.Errorf("expected error for ftp:// scheme")
	}
}

func TestIsPublicIP(t *testing.T) {
	tests := []struct {
		ip     string
		public bool
	}{
		{"8.8.8.8", true},
		{"1.1.1.1", true},
		{"142.250.190.46", true},
		{"2607:f8b0:4005:805::200e", true},
		// Loopback
		{"127.0.0.1", false},
		{"127.0.0.2", false},
		{"::1", false},
		{"::ffff:127.0.0.1", false},
		// Cloud Metadata / Link-local
		{"169.254.169.254", false},
		{"169.254.0.1", false},
		{"fe80::1", false},
		// Private RFC 1918
		{"10.0.0.1", false},
		{"172.16.0.1", false},
		{"172.31.255.255", false},
		{"192.168.1.1", false},
		{"fc00::1", false},
		// CGNAT
		{"100.64.0.1", false},
		{"100.127.255.255", false},
		// Benchmark / Documentation
		{"198.18.0.1", false},
		{"192.0.2.1", false},
		{"198.51.100.1", false},
		{"203.0.113.1", false},
		{"2001:db8::1", false},
		// Multicast / Broadcast
		{"224.0.0.1", false},
		{"255.255.255.255", false},
		{"0.0.0.0", false},
		{"::", false},
	}

	for _, tt := range tests {
		parsed := net.ParseIP(tt.ip)
		if parsed == nil {
			t.Fatalf("failed to parse IP %s", tt.ip)
		}
		got := IsPublicIP(parsed)
		if got != tt.public {
			t.Errorf("IsPublicIP(%s) = %v, want %v", tt.ip, got, tt.public)
		}
	}
}

func TestValidateRemoteImageURL(t *testing.T) {
	tests := []struct {
		raw     string
		wantErr bool
	}{
		{"https://example.com/image.png", false},
		{"http://example.com/image.jpg", false},
		{"https://example.com:443/image.webp", false},
		{"http://example.com:80/image.webp", false},
		// Credentials not allowed
		{"https://user:pass@example.com/image.png", true},
		// Non-standard ports rejected
		{"https://example.com:8443/image.png", true},
		{"http://example.com:8080/image.png", true},
		// Private IP literals rejected
		{"http://127.0.0.1/image.png", true},
		{"http://169.254.169.254/latest/meta-data", true},
		{"http://10.0.0.1/image.png", true},
		{"http://192.168.1.1/image.png", true},
		// Bad schemes
		{"ftp://example.com/image.png", true},
		{"javascript:alert(1)", true},
	}

	for _, tt := range tests {
		u, err := url.Parse(tt.raw)
		if err != nil {
			if !tt.wantErr {
				t.Fatalf("unexpected url.Parse error for %s: %v", tt.raw, err)
			}
			continue
		}
		err = ValidateRemoteImageURL(u)
		if (err != nil) != tt.wantErr {
			t.Errorf("ValidateRemoteImageURL(%s) error = %v, wantErr %v", tt.raw, err, tt.wantErr)
		}
	}
}

func TestValidateHostPublic(t *testing.T) {
	ctx := context.Background()

	// localhost and 127.0.0.1 should fail
	if err := ValidateHostPublic(ctx, "localhost"); err == nil {
		t.Errorf("expected localhost to be rejected as non-public")
	}
	if err := ValidateHostPublic(ctx, "127.0.0.1"); err == nil {
		t.Errorf("expected 127.0.0.1 to be rejected as non-public")
	}
	if err := ValidateHostPublic(ctx, "169.254.169.254"); err == nil {
		t.Errorf("expected 169.254.169.254 to be rejected as non-public")
	}
}

func TestFetchImageBytesSSRFRejection(t *testing.T) {
	ctx := context.Background()

	// Loopback address
	_, err := FetchImageBytes(ctx, nil, "http://127.0.0.1:8080/secret.png")
	if err == nil {
		t.Errorf("expected error fetching loopback URL")
	}

	// Cloud metadata IP
	_, err = FetchImageBytes(ctx, nil, "http://169.254.169.254/latest/meta-data")
	if err == nil {
		t.Errorf("expected error fetching cloud metadata URL")
	}

	// Localhost hostname
	_, err = FetchImageBytes(ctx, nil, "http://localhost/image.png")
	if err == nil {
		t.Errorf("expected error fetching localhost URL")
	}
}
