package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gomini/internal/gemini"
)

type mockGemini struct {
	genFunc       func(ctx context.Context, prompt string, modelID, thinkMode int, fileRefs []string, extra map[int]int) (string, error)
	genStreamFunc func(ctx context.Context, prompt string, modelID, thinkMode int, fileRefs []string, extra map[int]int) <-chan gemini.StreamEvent
}

func (m *mockGemini) Generate(ctx context.Context, prompt string, modelID, thinkMode int, fileRefs []string, extra map[int]int) (string, error) {
	if m.genFunc != nil {
		return m.genFunc(ctx, prompt, modelID, thinkMode, fileRefs, extra)
	}
	return "mock response", nil
}

func (m *mockGemini) GenerateStream(ctx context.Context, prompt string, modelID, thinkMode int, fileRefs []string, extra map[int]int) <-chan gemini.StreamEvent {
	if m.genStreamFunc != nil {
		return m.genStreamFunc(ctx, prompt, modelID, thinkMode, fileRefs, extra)
	}
	ch := make(chan gemini.StreamEvent, 2)
	ch <- gemini.StreamEvent{Delta: "hel"}
	ch <- gemini.StreamEvent{Delta: "lo"}
	close(ch)
	return ch
}

func (m *mockGemini) SetLogger(func(string)) {}

type mockUploader struct {
	uploadedRefs []string
	uploadErr    error
}

func (u *mockUploader) UploadImage(ctx context.Context, imageBytes []byte, mimeType string) (string, error) {
	if u.uploadErr != nil {
		return "", u.uploadErr
	}
	ref := "/uploaded/mock-image"
	u.uploadedRefs = append(u.uploadedRefs, ref)
	return ref, nil
}

func (u *mockUploader) FetchImageBytes(ctx context.Context, urlStr string) ([]byte, error) {
	return []byte("fake remote bytes"), nil
}

func TestHealthEndpoint(t *testing.T) {
	s := New(NewConfig(nil, "gemini-3.6-flash", false), &mockGemini{}, &mockUploader{})

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var res map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if res["status"] != "ok" {
		t.Errorf("expected status ok, got %v", res["status"])
	}
}

func TestChatStreamStartsWithAssistantRole(t *testing.T) {
	mockClient := &mockGemini{}
	s := New(NewConfig(nil, "gemini-3.6-flash", false), mockClient, &mockUploader{})

	reqBody := `{"model":"gemini-3.7-flash","messages":[{"role":"user","content":"hello"}],"stream":true}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(reqBody))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	scanner := bufio.NewScanner(w.Body)
	var chunks []map[string]any
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: {") {
			var chunk map[string]any
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &chunk); err == nil {
				chunks = append(chunks, chunk)
			}
		}
	}

	if len(chunks) < 3 {
		t.Fatalf("expected at least 3 chunks, got %d", len(chunks))
	}

	// First chunk MUST have choices[0].delta = {"role": "assistant"}
	choices0, _ := chunks[0]["choices"].([]any)
	c0Map, _ := choices0[0].(map[string]any)
	delta0, _ := c0Map["delta"].(map[string]any)
	if delta0["role"] != "assistant" {
		t.Errorf("expected first chunk delta role: assistant, got %v", delta0["role"])
	}

	// Subsequent chunks have delta content
	choices1, _ := chunks[1]["choices"].([]any)
	c1Map, _ := choices1[0].(map[string]any)
	delta1, _ := c1Map["delta"].(map[string]any)
	if delta1["content"] != "hel" {
		t.Errorf("expected second chunk content 'hel', got %v", delta1["content"])
	}
}

func TestResponsesStreamFullEventSequence(t *testing.T) {
	mockClient := &mockGemini{
		genFunc: func(ctx context.Context, prompt string, modelID, thinkMode int, fileRefs []string, extra map[int]int) (string, error) {
			return "hello", nil
		},
	}
	s := New(NewConfig(nil, "gemini-3.6-flash", false), mockClient, &mockUploader{})

	reqBody := `{"model":"gemini-3.7-flash","input":"hello","stream":true}`
	req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(reqBody))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	scanner := bufio.NewScanner(w.Body)
	type sseEvent struct {
		Event string
		Data  map[string]any
	}
	var events []sseEvent
	var curEvent string

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			curEvent = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") && curEvent != "" {
			var data map[string]any
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &data); err == nil {
				events = append(events, sseEvent{Event: curEvent, Data: data})
			}
			curEvent = ""
		}
	}

	expectedTypes := []string{
		"response.created",
		"response.in_progress",
		"response.output_item.added",
		"response.content_part.added",
		"response.output_text.delta",
		"response.output_text.done",
		"response.content_part.done",
		"response.output_item.done",
		"response.completed",
	}

	if len(events) != len(expectedTypes) {
		t.Fatalf("expected %d events, got %d", len(expectedTypes), len(events))
	}

	for i, exp := range expectedTypes {
		if events[i].Event != exp {
			t.Errorf("event %d: expected %s, got %s", i, exp, events[i].Event)
		}
		seq, ok := events[i].Data["sequence_number"].(float64)
		if !ok || int(seq) != i+1 {
			t.Errorf("event %d: expected sequence_number %d, got %v", i, i+1, events[i].Data["sequence_number"])
		}
	}

	// Check delta content
	if events[4].Data["delta"] != "hello" {
		t.Errorf("expected delta hello, got %v", events[4].Data["delta"])
	}
}

func TestChatMultimodalUpload(t *testing.T) {
	uploader := &mockUploader{}
	var receivedRefs []string
	mockClient := &mockGemini{
		genFunc: func(ctx context.Context, prompt string, modelID, thinkMode int, fileRefs []string, extra map[int]int) (string, error) {
			receivedRefs = fileRefs
			return "image analyzed", nil
		},
	}
	s := New(NewConfig(nil, "gemini-3.6-flash", false), mockClient, uploader)

	b := bytes.NewBufferString(`{
		"model": "gemini-3.7-flash",
		"messages": [{
			"role": "user",
			"content": [
				{"type": "text", "text": "Describe this"},
				{"type": "image_url", "image_url": {"url": "data:image/png;base64,ZmFrZQ=="}}
			]
		}],
		"stream": false
	}`)
	req := httptest.NewRequest("POST", "/v1/chat/completions", b)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(receivedRefs) != 1 || receivedRefs[0] != "/uploaded/mock-image" {
		t.Errorf("expected fileRefs [/uploaded/mock-image], got %v", receivedRefs)
	}
}
