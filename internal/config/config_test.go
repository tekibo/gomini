package config

import (
	"encoding/json"
	"testing"
)

func TestTemporaryChatsDefault(t *testing.T) {
	cfg := Default()
	if cfg.TemporaryChats {
		t.Errorf("expected TemporaryChats to default to false")
	}
}

func TestTemporaryChatsFromJSON(t *testing.T) {
	jsonBlob := `{"temporary_chats": true}`
	cfg := Default()
	if err := json.Unmarshal([]byte(jsonBlob), cfg); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if !cfg.TemporaryChats {
		t.Errorf("expected TemporaryChats to be true")
	}
}

func TestTemporaryChatsFromEnv(t *testing.T) {
	t.Setenv("GOMINI_TEMPORARY_CHATS", "true")

	cfg := Default()
	cfg.ApplyEnv()
	if !cfg.TemporaryChats {
		t.Errorf("expected TemporaryChats from env to be true")
	}
}

func TestModelTicketConfig(t *testing.T) {
	cfg := Default()
	if !cfg.SendModelTicket {
		t.Errorf("expected SendModelTicket default to be true")
	}
	if cfg.TicketTimeoutSec != 25 {
		t.Errorf("expected TicketTimeoutSec default 25, got %d", cfg.TicketTimeoutSec)
	}
	if cfg.IdleTimeoutSec != 90 {
		t.Errorf("expected IdleTimeoutSec default 90, got %d", cfg.IdleTimeoutSec)
	}

	t.Setenv("GOMINI_SEND_MODEL_TICKET", "false")
	t.Setenv("GOMINI_TICKET_TIMEOUT_SEC", "30")
	t.Setenv("GOMINI_IDLE_TIMEOUT_SEC", "120")
	cfg.ApplyEnv()
	if cfg.SendModelTicket {
		t.Errorf("expected SendModelTicket to be false")
	}
	if cfg.TicketTimeoutSec != 30 {
		t.Errorf("expected TicketTimeoutSec to be 30, got %d", cfg.TicketTimeoutSec)
	}
	if cfg.IdleTimeoutSec != 120 {
		t.Errorf("expected IdleTimeoutSec to be 120, got %d", cfg.IdleTimeoutSec)
	}
}

func TestFlagOverridesConfigFile(t *testing.T) {
	cfg := Default()
	// config.json in root sets port to 4982
	// CLI arg should override it to 8081
	if err := cfg.ApplyArgs([]string{"-port", "8081"}); err != nil {
		t.Fatalf("ApplyArgs error: %v", err)
	}
	if cfg.Port != 8081 {
		t.Errorf("expected port 8081 from CLI flags to override config.json, got %d", cfg.Port)
	}
}
