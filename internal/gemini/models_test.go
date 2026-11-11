package gemini

import (
	"testing"
)

func TestModelCatalog(t *testing.T) {
	cfg, ok := Models["gemini-3.7-flash"]
	if !ok {
		t.Fatalf("gemini-3.7-flash not found in Models")
	}
	if cfg.Mode != 1 || cfg.Think != 4 {
		t.Errorf("expected mode=1 think=4, got mode=%d think=%d", cfg.Mode, cfg.Think)
	}
	if cfg.Desc != "Latest all-around model (Gemini 3.7 Flash)" {
		t.Errorf("unexpected description: %s", cfg.Desc)
	}

	cfg36, ok := Models["gemini-3.6-flash"]
	if !ok {
		t.Fatalf("gemini-3.6-flash not found in Models")
	}
	if cfg36.Desc != "All-around model (Gemini 3.6 Flash)" {
		t.Errorf("unexpected description: %s", cfg36.Desc)
	}
}

func TestResolveModelWithThinkOverride(t *testing.T) {
	name, mode, think, _, err := ResolveModel("gemini-3.7-flash@think=0", "gemini-3.6-flash")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "gemini-3.7-flash" {
		t.Errorf("expected name gemini-3.7-flash, got %s", name)
	}
	if mode != 1 {
		t.Errorf("expected mode 1, got %d", mode)
	}
	if think != 0 {
		t.Errorf("expected think 0, got %d", think)
	}
}
