package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"gomini/internal/gemini"
	"gomini/internal/tools"
)

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model      string          `json:"model"`
		Messages   json.RawMessage `json:"messages"`
		Tools      json.RawMessage `json:"tools,omitempty"`
		ToolChoice any             `json:"tool_choice,omitempty"`
		Stream     bool            `json:"stream"`
	}

	if err := parseBody(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorObj(err.Error()))
		return
	}

	modelName := req.Model
	if modelName == "" {
		modelName = s.config.DefaultModel
	}

	resolvedName, modelID, thinkMode, extra, err := gemini.ResolveModel(modelName, s.config.DefaultModel)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorObj(err.Error()))
		return
	}

	var messages []tools.Message
	if req.Messages != nil {
		if err := json.Unmarshal(req.Messages, &messages); err != nil {
			writeJSON(w, http.StatusBadRequest, errorObj("invalid messages"))
			return
		}
	}

	var toolDefs []tools.ToolDef
	if req.Tools != nil {
		var rawTools []json.RawMessage
		if err := json.Unmarshal(req.Tools, &rawTools); err == nil {
			for _, t := range rawTools {
				var tool struct {
					Type     string          `json:"type"`
					Function json.RawMessage `json:"function,omitempty"`
				}
				if err := json.Unmarshal(t, &tool); err != nil {
					continue
				}
				fn := tool.Function
				if tool.Type == "function" && len(fn) > 0 {
					var td tools.ToolDef
					if err := json.Unmarshal(fn, &td); err == nil {
						toolDefs = append(toolDefs, td)
					}
				} else if tool.Type == "function" {
					var td tools.ToolDef
					if err := json.Unmarshal(t, &td); err == nil {
						toolDefs = append(toolDefs, td)
					}
				}
			}
		}
	}

	toolChoice := req.ToolChoice
	if toolChoice == nil {
		toolChoice = "auto"
	}

	prompt, images := tools.MessagesToPrompt(messages, toolDefs, toolChoice)
	if prompt == "" {
		writeJSON(w, http.StatusBadRequest, errorObj("empty prompt"))
		return
	}

	cid := "chatcmpl-" + hexID(12)
	created := time.Now().Unix()

	if req.Stream && (len(toolDefs) == 0 || toolChoice == "none") {
		s.streamChat(w, r.Context(), cid, created, resolvedName, modelID, thinkMode, extra, prompt, images)
		return
	}

	fileRefs, err := s.uploadImages(r.Context(), images)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errorObj("upstream error: "+err.Error()))
		return
	}
	text, err := s.gemini.Generate(r.Context(), prompt, modelID, thinkMode, fileRefs, extra)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errorObj("upstream error: "+err.Error()))
		return
	}

	var toolCalls []tools.ToolCall
	if len(toolDefs) > 0 && text != "" && toolChoice != "none" {
		text, toolCalls = tools.ParseToolCalls(text)
	}

	msg := map[string]any{
		"role":    "assistant",
		"content": nil,
	}
	if text != "" {
		msg["content"] = text
	}
	finish := "stop"
	if len(toolCalls) > 0 {
		msg["tool_calls"] = toolCalls
		finish = "tool_calls"
	}

	if req.Stream {
		startSSE(w)
		chunk := map[string]any{
			"id":      cid,
			"object":  "chat.completion.chunk",
			"created": created,
			"model":   resolvedName,
			"choices": []map[string]any{
				{"index": 0, "delta": msg, "finish_reason": finish},
			},
		}
		_ = writeSSE(w, chunk)
		_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":      cid,
		"object":  "chat.completion",
		"created": created,
		"model":   resolvedName,
		"choices": []map[string]any{
			{
				"index":         0,
				"message":       msg,
				"finish_reason": finish,
			},
		},
		"usage": map[string]int{
			"prompt_tokens":     usageTokens(prompt),
			"completion_tokens": usageTokens(text),
			"total_tokens":      usageTokens(prompt + text),
		},
	})
}

func (s *Server) streamChat(w http.ResponseWriter, ctx context.Context, cid string, created int64, modelName string, modelID, thinkMode int, extra map[int]int, prompt string, images []tools.ImageData) {
	fileRefs, err := s.uploadImages(ctx, images)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errorObj("upstream error: "+err.Error()))
		return
	}
	ch := s.gemini.GenerateStream(ctx, prompt, modelID, thinkMode, fileRefs, extra)

	startSSE(w)
	flusher, ok := w.(http.Flusher)
	if !ok {
		flusher = nopFlusher{}
	}

	firstChunk := map[string]any{
		"id":      cid,
		"object":  "chat.completion.chunk",
		"created": created,
		"model":   modelName,
		"choices": []map[string]any{
			{"index": 0, "delta": map[string]string{"role": "assistant"}, "finish_reason": nil},
		},
	}
	_ = writeSSE(w, firstChunk)
	flusher.Flush()

	for evt := range ch {
		if evt.Err != nil {
			return
		}
		if evt.Delta == "" {
			continue
		}
		chunk := map[string]any{
			"id":      cid,
			"object":  "chat.completion.chunk",
			"created": created,
			"model":   modelName,
			"choices": []map[string]any{
				{"index": 0, "delta": map[string]string{"content": evt.Delta}, "finish_reason": nil},
			},
		}
		if err := writeSSE(w, chunk); err != nil {
			return
		}
		flusher.Flush()
	}

	end := map[string]any{
		"id":      cid,
		"object":  "chat.completion.chunk",
		"created": created,
		"model":   modelName,
		"choices": []map[string]any{
			{"index": 0, "delta": map[string]string{}, "finish_reason": "stop"},
		},
	}
	_ = writeSSE(w, end)
	_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}

type nopFlusher struct{}

func (nopFlusher) Flush() {}

func (s *Server) uploadImages(ctx context.Context, images []tools.ImageData) ([]string, error) {
	if s.uploader == nil || len(images) == 0 {
		return nil, nil
	}
	var refs []string
	for _, img := range images {
		data := img.Data
		if len(data) == 0 && img.URL != "" {
			fetched, err := s.uploader.FetchImageBytes(ctx, img.URL)
			if err != nil {
				return nil, fmt.Errorf("image fetch failed: %w", err)
			}
			data = fetched
		}
		if len(data) == 0 {
			return nil, fmt.Errorf("image fetch failed")
		}
		ref, err := s.uploader.UploadImage(ctx, data, img.MIMEType)
		if err != nil {
			return nil, fmt.Errorf("image upload failed: %w", err)
		}
		refs = append(refs, ref)
	}
	return refs, nil
}
