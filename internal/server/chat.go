package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"gomini/internal/gemini"
	"gomini/internal/tools"
)

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model           string          `json:"model"`
		Messages        json.RawMessage `json:"messages"`
		Tools           json.RawMessage `json:"tools,omitempty"`
		ToolChoice      any             `json:"tool_choice,omitempty"`
		Stream          bool            `json:"stream"`
		ReasoningEffort string          `json:"reasoning_effort,omitempty"`
	}

	if err := parseBody(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorObj(err.Error()))
		return
	}

	modelName := req.Model
	if modelName == "" {
		modelName = s.config.DefaultModel
	}

	resolvedName, modelID, thinkMode, extra, ticketHdr, err := gemini.ResolveModel(modelName, gemini.ResolveOptions{
		DefaultModel:    s.config.DefaultModel,
		ReasoningEffort: req.ReasoningEffort,
		ModelTickets:    s.config.ModelTickets,
		SendModelTicket: s.config.SendModelTicket,
	})
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
	hasTools := len(toolDefs) > 0 && toolChoice != "none"

	if req.Stream {
		s.streamChat(r.Context(), w, cid, created, resolvedName, modelID, thinkMode, extra, ticketHdr, prompt, images, toolDefs, toolChoice)
		return
	}

	fileRefs, err := s.uploadImages(r.Context(), images)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errorObj("upstream error: "+err.Error()))
		return
	}

	text, err := s.gemini.Generate(r.Context(), prompt, modelID, thinkMode, fileRefs, extra, ticketHdr)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errorObj("upstream error: "+err.Error()))
		return
	}

	// Canned error retry
	if tools.ClassifyCanned(text) == "failure" {
		for attempt := 0; attempt < 2; attempt++ {
			t, retryErr := s.gemini.Generate(r.Context(), prompt, modelID, thinkMode, fileRefs, extra, ticketHdr)
			if retryErr == nil && tools.ClassifyCanned(t) != "failure" {
				text = t
				break
			}
		}
	}

	// Tutorial phrasing corrective retry when tools are active
	if hasTools && tools.LooksLikeTutorial(text) {
		correctedPrompt := prompt + "\n\n[Correction]: Please do not explain the steps. Use the provided tools to make the changes."
		t, retryErr := s.gemini.Generate(r.Context(), correctedPrompt, modelID, thinkMode, fileRefs, extra, ticketHdr)
		if retryErr == nil && !tools.LooksLikeTutorial(t) {
			text = t
		}
	}

	var toolCalls []tools.ToolCall
	if hasTools && text != "" {
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

func (s *Server) streamChat(ctx context.Context, w http.ResponseWriter, cid string, created int64, modelName string, modelID, thinkMode int, extra map[int]int, ticketHdr, prompt string, images []tools.ImageData, toolDefs []tools.ToolDef, toolChoice any) {
	fileRefs, err := s.uploadImages(ctx, images)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errorObj("upstream error: "+err.Error()))
		return
	}

	startSSE(w)
	flusher, ok := w.(http.Flusher)
	if !ok {
		flusher = nopFlusher{}
	}

	sendChunk := func(delta map[string]any, finish any) {
		chunk := map[string]any{
			"id":      cid,
			"object":  "chat.completion.chunk",
			"created": created,
			"model":   modelName,
			"choices": []map[string]any{
				{"index": 0, "delta": delta, "finish_reason": finish},
			},
		}
		_ = writeSSE(w, chunk)
		flusher.Flush()
	}

	sendChunk(map[string]any{"role": "assistant"}, nil)

	hasTools := len(toolDefs) > 0 && toolChoice != "none"
	ch := s.gemini.GenerateStream(ctx, prompt, modelID, thinkMode, fileRefs, extra, ticketHdr)

	if !hasTools {
		for evt := range ch {
			if evt.Err != nil {
				sendChunk(map[string]any{"content": fmt.Sprintf("[error] %v", evt.Err)}, "stop")
				_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
				flusher.Flush()
				return
			}
			if evt.Delta != "" {
				sendChunk(map[string]any{"content": evt.Delta}, nil)
			}
		}
		sendChunk(map[string]any{}, "stop")
		_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
		return
	}

	fullText := ""
	emitted := 0
	markerPos := -1

	for evt := range ch {
		if evt.Err != nil {
			sendChunk(map[string]any{"content": fmt.Sprintf("[error] %v", evt.Err)}, "stop")
			_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
			flusher.Flush()
			return
		}
		if evt.Delta == "" {
			continue
		}
		fullText += evt.Delta
		if markerPos == -1 {
			markerPos = strings.Index(fullText, tools.ToolCallMarker)
		}
		var limit int
		if markerPos != -1 {
			limit = markerPos
		} else {
			limit = len(fullText) - len(tools.ToolCallMarker) + 1
			if limit < 0 {
				limit = 0
			}
		}
		if limit > emitted {
			sendChunk(map[string]any{"content": fullText[emitted:limit]}, nil)
			emitted = limit
		}
	}

	clean, toolCalls := tools.ParseToolCalls(fullText)
	if len(toolCalls) > 0 {
		if len(clean) > emitted {
			sendChunk(map[string]any{"content": clean[emitted:]}, nil)
		}
		for i, tc := range toolCalls {
			tcMap := map[string]any{
				"index": i,
				"id":    tc.ID,
				"type":  "function",
				"function": map[string]string{
					"name":      tc.Function.Name,
					"arguments": tc.Function.Arguments,
				},
			}
			sendChunk(map[string]any{"tool_calls": []any{tcMap}}, nil)
		}
		sendChunk(map[string]any{}, "tool_calls")
	} else {
		if len(fullText) > emitted {
			sendChunk(map[string]any{"content": fullText[emitted:]}, nil)
		}
		sendChunk(map[string]any{}, "stop")
	}

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
