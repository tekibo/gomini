package server

import (
	"encoding/json"
	"net/http"
	"time"

	"gomini/internal/gemini"
	"gomini/internal/tools"
)

func (s *Server) handleResponses(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model        string          `json:"model"`
		Instructions string          `json:"instructions"`
		Input        json.RawMessage `json:"input"`
		Tools        json.RawMessage `json:"tools,omitempty"`
		ToolChoice   any             `json:"tool_choice,omitempty"`
		Stream       bool            `json:"stream"`
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
	if req.Instructions != "" {
		messages = append(messages, tools.Message{Role: "system", Content: req.Instructions})
	}

	if len(req.Input) > 0 {
		var inputStr string
		if err := json.Unmarshal(req.Input, &inputStr); err == nil {
			messages = append(messages, tools.Message{Role: "user", Content: inputStr})
		} else {
			var inputItems []json.RawMessage
			if err := json.Unmarshal(req.Input, &inputItems); err == nil {
				for _, item := range inputItems {
					var strItem string
					if err := json.Unmarshal(item, &strItem); err == nil {
						messages = append(messages, tools.Message{Role: "user", Content: strItem})
						continue
					}
					var itemObj struct {
						Type      string          `json:"type"`
						Role      string          `json:"role"`
						Content   json.RawMessage `json:"content"`
						CallID    string          `json:"call_id"`
						Name      string          `json:"name"`
						Output    string          `json:"output"`
						Arguments string          `json:"arguments"`
					}
					if err := json.Unmarshal(item, &itemObj); err != nil {
						continue
					}
					switch itemObj.Type {
					case "function_call_output":
						messages = append(messages, tools.Message{
							Role:       "tool",
							ToolCallID: itemObj.CallID,
							Name:       itemObj.Name,
							Content:    itemObj.Output,
						})
					case "input_text", "input_image", "image", "image_url":
						var part map[string]any
						_ = json.Unmarshal(item, &part)
						messages = append(messages, tools.Message{Role: "user", Content: []any{part}})
					default:
						role := itemObj.Role
						if role == "" {
							role = "user"
						}
						if len(itemObj.Content) > 0 {
							var contentStr string
							if err := json.Unmarshal(itemObj.Content, &contentStr); err == nil {
								messages = append(messages, tools.Message{Role: role, Content: contentStr})
							} else {
								var contentParts []any
								if err := json.Unmarshal(itemObj.Content, &contentParts); err == nil {
									messages = append(messages, tools.Message{Role: role, Content: contentParts})
								}
							}
						}
					}
				}
			}
		}
	}

	var toolDefs []tools.ToolDef
	if req.Tools != nil {
		var rawTools []json.RawMessage
		if err := json.Unmarshal(req.Tools, &rawTools); err == nil {
			for _, t := range rawTools {
				var td tools.ToolDef
				if err := json.Unmarshal(t, &td); err == nil {
					toolDefs = append(toolDefs, td)
				} else {
					var fn struct {
						Name        string `json:"name"`
						Description string `json:"description"`
						Parameters  any    `json:"parameters"`
					}
					if err := json.Unmarshal(t, &fn); err == nil {
						toolDefs = append(toolDefs, tools.ToolDef{
							Name:        fn.Name,
							Description: fn.Description,
							Parameters:  fn.Parameters,
						})
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
		writeJSON(w, http.StatusBadRequest, errorObj("empty input"))
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

	rid := "resp_" + hexID(16)
	mid := "msg_" + hexID(12)

	var output []map[string]any
	for _, tc := range toolCalls {
		output = append(output, map[string]any{
			"type":      "function_call",
			"id":        tc.ID,
			"call_id":   tc.ID,
			"name":      tc.Function.Name,
			"arguments": tc.Function.Arguments,
			"status":    "completed",
		})
	}
	if text != "" || len(toolCalls) == 0 {
		output = append(output, map[string]any{
			"type":   "message",
			"id":     mid,
			"role":   "assistant",
			"status": "completed",
			"content": []map[string]any{
				{"type": "output_text", "text": text, "annotations": []any{}},
			},
		})
	}

	usage := map[string]int{
		"input_tokens":  usageTokens(prompt),
		"output_tokens": usageTokens(text),
		"total_tokens":  usageTokens(prompt + text),
	}

	if req.Stream {
		startSSE(w)
		flusher, _ := w.(http.Flusher)

		seq := 0
		emit := func(evType string, fields map[string]any) {
			seq++
			fields["type"] = evType
			fields["sequence_number"] = seq
			_ = writeSSEEvent(w, evType, fields)
			if flusher != nil {
				flusher.Flush()
			}
		}

		baseResp := map[string]any{
			"id":         rid,
			"object":     "response",
			"created_at": time.Now().Unix(),
			"model":      resolvedName,
		}

		createdResp := make(map[string]any)
		for k, v := range baseResp {
			createdResp[k] = v
		}
		createdResp["status"] = "in_progress"
		createdResp["output"] = []any{}
		createdResp["usage"] = nil

		emit("response.created", map[string]any{"response": createdResp})
		emit("response.in_progress", map[string]any{"response": createdResp})

		for oi, item := range output {
			switch item["type"] {
			case "function_call":
				pending := map[string]any{
					"type":      "function_call",
					"id":        item["id"],
					"call_id":   item["call_id"],
					"name":      item["name"],
					"arguments": "",
					"status":    "in_progress",
				}
				emit("response.output_item.added", map[string]any{
					"output_index": oi,
					"item":         pending,
				})
				emit("response.function_call_arguments.delta", map[string]any{
					"item_id":      item["id"],
					"output_index": oi,
					"delta":        item["arguments"],
				})
				emit("response.function_call_arguments.done", map[string]any{
					"item_id":      item["id"],
					"output_index": oi,
					"arguments":    item["arguments"],
				})
				emit("response.output_item.done", map[string]any{
					"output_index": oi,
					"item":         item,
				})
			case "message":
				pending := map[string]any{
					"type":    "message",
					"id":      item["id"],
					"role":    "assistant",
					"status":  "in_progress",
					"content": []any{},
				}
				emit("response.output_item.added", map[string]any{
					"output_index": oi,
					"item":         pending,
				})
				content, _ := item["content"].([]map[string]any)
				for ci, cp := range content {
					emit("response.content_part.added", map[string]any{
						"item_id":       item["id"],
						"output_index":  oi,
						"content_index": ci,
						"part": map[string]any{
							"type":        "output_text",
							"text":        "",
							"annotations": []any{},
						},
					})
					emit("response.output_text.delta", map[string]any{
						"item_id":       item["id"],
						"output_index":  oi,
						"content_index": ci,
						"delta":         cp["text"],
					})
					emit("response.output_text.done", map[string]any{
						"item_id":       item["id"],
						"output_index":  oi,
						"content_index": ci,
						"text":          cp["text"],
					})
					emit("response.content_part.done", map[string]any{
						"item_id":       item["id"],
						"output_index":  oi,
						"content_index": ci,
						"part":          cp,
					})
				}
				emit("response.output_item.done", map[string]any{
					"output_index": oi,
					"item":         item,
				})
			}
		}

		completedResp := make(map[string]any)
		for k, v := range baseResp {
			completedResp[k] = v
		}
		completedResp["status"] = "completed"
		completedResp["output"] = output
		completedResp["usage"] = usage

		emit("response.completed", map[string]any{"response": completedResp})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":         rid,
		"object":     "response",
		"created_at": time.Now().Unix(),
		"status":     "completed",
		"model":      resolvedName,
		"output":     output,
		"usage":      usage,
	})
}
