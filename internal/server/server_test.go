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
	genFunc       func(ctx context.Context, prompt string, modelID, thinkMode int, fileRefs []string, extra map[int]int, ticketHeader ...string) (string, error)
	genStreamFunc func(ctx context.Context, prompt string, modelID, thinkMode int, fileRefs []string, extra map[int]int, ticketHeader ...string) <-chan gemini.StreamEvent
}

func (m *mockGemini) Generate(ctx context.Context, prompt string, modelID, thinkMode int, fileRefs []string, extra map[int]int, ticketHeader ...string) (string, error) {
	if m.genFunc != nil {
		return m.genFunc(ctx, prompt, modelID, thinkMode, fileRefs, extra, ticketHeader...)
	}
	return "mock response", nil
}

func (m *mockGemini) GenerateStream(ctx context.Context, prompt string, modelID, thinkMode int, fileRefs []string, extra map[int]int, ticketHeader ...string) <-chan gemini.StreamEvent {
	if m.genStreamFunc != nil {
		return m.genStreamFunc(ctx, prompt, modelID, thinkMode, fileRefs, extra, ticketHeader...)
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

func (u *mockUploader) UploadImage(_ context.Context, _ []byte, _ string) (string, error) {
	if u.uploadErr != nil {
		return "", u.uploadErr
	}
	ref := "/uploaded/mock-image"
	u.uploadedRefs = append(u.uploadedRefs, ref)
	return ref, nil
}

func (u *mockUploader) FetchImageBytes(_ context.Context, _ string) ([]byte, error) {
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
		genFunc: func(_ context.Context, _ string, _, _ int, _ []string, _ map[int]int, _ ...string) (string, error) {
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
		genFunc: func(_ context.Context, _ string, _, _ int, fileRefs []string, _ map[int]int, _ ...string) (string, error) {
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

func TestChatStreamWithTools(t *testing.T) {
	mockClient := &mockGemini{
		genStreamFunc: func(_ context.Context, _ string, _, _ int, _ []string, _ map[int]int, _ ...string) <-chan gemini.StreamEvent {
			ch := make(chan gemini.StreamEvent, 2)
			ch <- gemini.StreamEvent{Delta: "Creating file.\n"}
			ch <- gemini.StreamEvent{Delta: "```tool_call\n{\"name\": \"write_file\", \"arguments\": {\"path\": \"a.txt\"}}\n```"}
			close(ch)
			return ch
		},
	}
	s := New(NewConfig(nil, "gemini-3.6-flash", false), mockClient, &mockUploader{})

	body := bytes.NewBufferString(`{
		"model": "gemini-3.6-flash",
		"stream": true,
		"tools": [{"type": "function", "function": {"name": "write_file", "parameters": {}}}],
		"messages": [{"role": "user", "content": "write file"}]
	}`)
	req := httptest.NewRequest("POST", "/v1/chat/completions", body)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	respStr := w.Body.String()

	// Parse SSE chunks to concatenate content deltas
	var accumulatedContent string
	var hasToolCallsDelta bool
	var hasToolCallsFinish bool

	scanner := bufio.NewScanner(strings.NewReader(respStr))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: {") {
			continue
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					ToolCalls []any  `json:"tool_calls"`
				} `json:"delta"`
				FinishReason any `json:"finish_reason"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &chunk); err == nil && len(chunk.Choices) > 0 {
			accumulatedContent += chunk.Choices[0].Delta.Content
			if len(chunk.Choices[0].Delta.ToolCalls) > 0 {
				hasToolCallsDelta = true
			}
			if chunk.Choices[0].FinishReason == "tool_calls" {
				hasToolCallsFinish = true
			}
		}
	}

	if accumulatedContent != "Creating file.\n" {
		t.Errorf("expected accumulated content 'Creating file.\n', got %q", accumulatedContent)
	}
	if !hasToolCallsDelta {
		t.Errorf("expected tool_calls delta in stream output")
	}
	if !hasToolCallsFinish {
		t.Errorf("expected finish_reason tool_calls in stream output")
	}
}

func TestCookiesSyncEndpoint(t *testing.T) {
	cs := &gemini.CookieStore{}
	cfg := NewConfig(nil, "gemini-3.6-flash", false)
	cfg.CookieStore = cs
	s := New(cfg, &mockGemini{}, &mockUploader{})

	// 1. Forbidden without extension header / chrome-extension origin
	reqNoHeader := httptest.NewRequest("POST", "/v1/cookies", bytes.NewBufferString(`{"cookie": "a=b"}`))
	wNoHeader := httptest.NewRecorder()
	s.ServeHTTP(wNoHeader, reqNoHeader)
	if wNoHeader.Code != http.StatusForbidden {
		t.Errorf("expected 403 without extension header, got %d", wNoHeader.Code)
	}

	// 2. Forbidden with evil host
	reqEvil := httptest.NewRequest("POST", "/v1/cookies", bytes.NewBufferString(`{"cookie": "a=b"}`))
	reqEvil.Host = "evil.com:8081"
	reqEvil.Header.Set("X-Gemini-Cookie-Sync", "1")
	wEvil := httptest.NewRecorder()
	s.ServeHTTP(wEvil, reqEvil)
	if wEvil.Code != http.StatusForbidden {
		t.Errorf("expected 403 with evil host, got %d", wEvil.Code)
	}

	// 3. Success with valid localhost and header
	body := `{"cookie": "SID=valid_sid; SAPISID=valid_sapisid", "model_tickets": {"gemini-4-argon": "95a4c0abcba24364"}}`
	reqOK := httptest.NewRequest("POST", "/v1/cookies", bytes.NewBufferString(body))
	reqOK.Host = "127.0.0.1:8081"
	reqOK.Header.Set("X-Gemini-Cookie-Sync", "1")
	wOK := httptest.NewRecorder()
	s.ServeHTTP(wOK, reqOK)

	if wOK.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", wOK.Code, wOK.Body.String())
	}
	if cfg.ModelTickets["gemini-4-argon"] != "95a4c0abcba24364" {
		t.Errorf("expected model ticket for gemini-4-argon to be updated")
	}
}

func TestAuthResiliencyAndErrorFormatting(t *testing.T) {
	cfg := NewConfig([]string{"secret-key-123"}, "gemini-3.6-flash", false)
	s := New(cfg, &mockGemini{}, &mockUploader{})

	// 1. Missing auth: check full OpenAI error JSON structure
	reqNoAuth := httptest.NewRequest("GET", "/v1/models", nil)
	wNoAuth := httptest.NewRecorder()
	s.ServeHTTP(wNoAuth, reqNoAuth)
	if wNoAuth.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", wNoAuth.Code)
	}
	var errResp struct {
		Error struct {
			Message string  `json:"message"`
			Type    string  `json:"type"`
			Param   *string `json:"param"`
			Code    string  `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(wNoAuth.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode error JSON: %v", err)
	}
	if errResp.Error.Message != "invalid api key" {
		t.Errorf("unexpected error message: %s", errResp.Error.Message)
	}
	if errResp.Error.Type != "invalid_request_error" {
		t.Errorf("unexpected error type: %s", errResp.Error.Type)
	}
	if errResp.Error.Code != "invalid_api_key" {
		t.Errorf("unexpected error code: %s", errResp.Error.Code)
	}

	// 2. Case-insensitive bearer and whitespace trimming variations
	authHeaders := []struct {
		header string
		val    string
	}{
		{"Authorization", "Bearer secret-key-123"},
		{"Authorization", "bearer secret-key-123"},
		{"Authorization", "  BEARER   secret-key-123  "},
		{"Authorization", "secret-key-123"},
		{"x-api-key", "secret-key-123"},
		{"x-api-key", "  secret-key-123  "},
	}

	for _, tt := range authHeaders {
		req := httptest.NewRequest("GET", "/v1/models", nil)
		req.Header.Set(tt.header, tt.val)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200 for auth %s: %q, got %d: %s", tt.header, tt.val, w.Code, w.Body.String())
		}
	}

	// 3. Wrong key
	reqWrong := httptest.NewRequest("GET", "/v1/models", nil)
	reqWrong.Header.Set("Authorization", "Bearer wrong-key")
	wWrong := httptest.NewRecorder()
	s.ServeHTTP(wWrong, reqWrong)
	if wWrong.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for wrong key, got %d", wWrong.Code)
	}
}
