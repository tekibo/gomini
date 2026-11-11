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
	name, mode, think, _, ticketHdr, err := ResolveModel("gemini-3.7-flash@think=0", ResolveOptions{DefaultModel: "gemini-3.6-flash"})
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
	_ = ticketHdr
}

func TestResolveModelArgonAndTickets(t *testing.T) {
	name, mode, think, _, ticketHdr, err := ResolveModel("gemini-4-argon", ResolveOptions{
		DefaultModel:    "gemini-3.6-flash",
		SendModelTicket: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "gemini-4-argon" {
		t.Errorf("expected gemini-4-argon, got %s", name)
	}
	if mode != 8 {
		t.Errorf("expected mode 8, got %d", mode)
	}
	if think != 2 {
		t.Errorf("expected think 2, got %d", think)
	}
	if ticketHdr == "" {
		t.Errorf("expected non-empty ticket header")
	}

	// Test reasoning effort
	name2, mode2, think2, _, _, err2 := ResolveModel("gemini-4-argon", ResolveOptions{
		ReasoningEffort: "high",
		SendModelTicket: true,
	})
	if err2 != nil || name2 != "gemini-4-argon" || mode2 != 8 || think2 != 0 {
		t.Errorf("expected argon with high reasoning effort to have think=0, got think=%d, err=%v", think2, err2)
	}

	// Test aliases
	name3, mode3, think3, _, _, err3 := ResolveModel("gemini-3.8-flash:high", ResolveOptions{})
	if err3 != nil || name3 != "gemini-3.8-flash-high" || mode3 != 2 || think3 != 0 {
		t.Errorf("expected 3.8-flash:high to resolve to gemini-3.8-flash-high, got %s mode=%d think=%d err=%v", name3, mode3, think3, err3)
	}
}
