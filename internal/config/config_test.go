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
