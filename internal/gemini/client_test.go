package gemini

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

func TestShortWrbLineParsed(t *testing.T) {
	// Regression: lines under 200 chars used to be skipped entirely
	inner := []any{nil, []string{"c_1", "r_1"}, nil, nil, []any{[]any{"cid", []string{"hi"}}}}
	innerBytes, _ := json.Marshal(inner)
	innerStr := string(innerBytes)

	first := []any{"wrb.fr", nil, innerStr}
	firstBytes, _ := json.Marshal(first)
	lineObj := []any{json.RawMessage(firstBytes)}
	lineBytes, _ := json.Marshal(lineObj)

	line := string(lineBytes)
	if len(line) >= 200 {
		t.Fatalf("expected short line < 200 chars, got %d", len(line))
	}

	texts := extractTextsFromLine(line)
	if len(texts) != 1 || texts[0] != "hi" {
		t.Errorf("expected ['hi'], got %v", texts)
	}
}

func TestFilePayloadStructure(t *testing.T) {
	cfg := &Config{TemporaryChats: false}
	c := NewClient(cfg)

	// File refs payload
	payload := c.buildPayload("look at this", 1, 4, []string{"/blob/123"}, nil, "req-uuid-1")
	values, err := url.ParseQuery(payload)
	if err != nil {
		t.Fatalf("failed to parse payload: %v", err)
	}

	freq := values.Get("f.req")
	var outer []any
	if err := json.Unmarshal([]byte(freq), &outer); err != nil {
		t.Fatalf("unmarshal outer error: %v", err)
	}

	innerStr, ok := outer[1].(string)
	if !ok {
		t.Fatalf("outer[1] is not string")
	}

	var inner []any
	if err := json.Unmarshal([]byte(innerStr), &inner); err != nil {
		t.Fatalf("unmarshal inner error: %v", err)
	}

	if len(inner) != 81 {
		t.Errorf("expected 81 slots for file request, got %d", len(inner))
	}
	if inner[80] != float64(1) {
		t.Errorf("expected slot 80 to be 1, got %v", inner[80])
	}
	if inner[59] != "req-uuid-1" {
		t.Errorf("expected slot 59 to be req-uuid-1, got %v", inner[59])
	}
}

func TestFormatBardError(t *testing.T) {
	err := formatBardError("1060")
	if !strings.Contains(err.Error(), "IP temporarily blocked") {
		t.Errorf("expected 'IP temporarily blocked', got %v", err)
	}
	err1037 := formatBardError("1037")
	if !strings.Contains(err1037.Error(), "usage limit exceeded") {
		t.Errorf("expected 'usage limit exceeded', got %v", err1037)
	}
}
