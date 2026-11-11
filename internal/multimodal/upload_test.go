package multimodal

import (
	"context"
	"net/http"
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
