package server

import (
	"encoding/json"
	"net/http"
	"strings"
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
		ToolChoice   any     `json:"tool_choice,omitempty"`
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
					case "message":
						role := itemObj.Role
						if role == "" {
							role = "user"
						}
						content := ""
						if itemObj.Content != nil {
							var contentStr string
							if err := json.Unmarshal(itemObj.Content, &contentStr); err == nil {
								content = contentStr
							} else {
								var contentParts []struct {
									Type string `json:"type"`
									Text string `json:"text"`
								}
								if err := json.Unmarshal(itemObj.Content, &contentParts); err == nil {
									var texts []string
									for _, cp := range contentParts {
										if cp.Type == "output_text" || cp.Type == "text" || cp.Type == "input_text" {
											texts = append(texts, cp.Text)
										}
									}
									content = strings.Join(texts, " ")
								}
							}
						}
						messages = append(messages, tools.Message{Role: role, Content: content})
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
						Name        string      `json:"name"`
						Description string      `json:"description"`
						Parameters  any `json:"parameters"`
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

	fileRefs := s.uploadImages(r.Context(), images)
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

		ev := map[string]any{
			"type": "response.created",
			"response": map[string]any{
				"id":     rid,
				"object": "response",
				"status": "in_progress",
				"model":  resolvedName,
				"output": []any{},
			},
		}
		_ = writeSSEEvent(w, "response.created", ev)
		if flusher != nil {
			flusher.Flush()
		}

		for _, item := range output {
			switch item["type"] {
			case "function_call":
				ev := map[string]any{
					"type":      "response.function_call_arguments.done",
					"item_id":   item["id"],
					"call_id":   item["call_id"],
					"name":      item["name"],
					"arguments": item["arguments"],
				}
				_ = writeSSEEvent(w, "response.function_call_arguments.done", ev)
				if flusher != nil {
					flusher.Flush()
				}
			case "message":
				content, _ := item["content"].([]map[string]any)
				for ci, cp := range content {
					ev := map[string]any{
						"type":          "response.output_text.done",
						"item_id":       item["id"],
						"content_index": ci,
						"text":          cp["text"],
					}
					_ = writeSSEEvent(w, "response.output_text.done", ev)
					if flusher != nil {
						flusher.Flush()
					}
				}
			}
		}

		respObj := map[string]any{
			"id":     rid,
			"object": "response",
			"status": "completed",
			"model":  resolvedName,
			"output": output,
			"usage":  usage,
		}
		_ = writeSSEEvent(w, "response.completed", map[string]any{
			"type":     "response.completed",
			"response": respObj,
		})
		if flusher != nil {
			flusher.Flush()
		}
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
