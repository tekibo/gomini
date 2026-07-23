package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"gomini/internal/gemini"
	"gomini/internal/tools"
)

func (s *Server) handleGoogleModels(w http.ResponseWriter, r *http.Request) {
	var models []map[string]any
	for name, cfg := range gemini.Models {
		models = append(models, map[string]any{
			"name":                       "models/" + name,
			"displayName":                name,
			"description":                cfg.Desc,
			"supportedGenerationMethods": []string{"generateContent", "streamGenerateContent"},
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": models})
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	var data []map[string]any
	for name, cfg := range gemini.Models {
		data = append(data, map[string]any{
			"id":          name,
			"object":      "model",
			"created":     1700000000,
			"owned_by":    "google",
			"description": cfg.Desc,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data":   data,
	})
}

func (s *Server) handleGoogleGenerate(stream bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := parseBody(r, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, errorObj(err.Error()))
			return
		}

		modelName := r.PathValue("model")
		if modelName == "" {
			modelName = s.config.DefaultModel
		}

		resolvedName, modelID, thinkMode, extra, err := gemini.ResolveModel(modelName, s.config.DefaultModel)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errorObj(err.Error()))
			return
		}

		var contents []tools.GoogleContent
		if rawContents, ok := req["contents"].([]any); ok {
			for _, rc := range rawContents {
				b, _ := json.Marshal(rc)
				var gc tools.GoogleContent
				if err := json.Unmarshal(b, &gc); err == nil {
					contents = append(contents, gc)
				}
			}
		}

		systemText := ""
		if si, ok := req["systemInstruction"].(map[string]any); ok {
			if parts, ok := si["parts"].([]any); ok {
				var texts []string
				for _, p := range parts {
					if part, ok := p.(map[string]any); ok {
						if t, ok := part["text"].(string); ok {
							texts = append(texts, t)
						}
					}
				}
				systemText = strings.Join(texts, " ")
			}
		}

		var toolDefs []tools.ToolDef
		fcMode := "AUTO"
		if tc, ok := req["toolConfig"].(map[string]any); ok {
			if fcc, ok := tc["functionCallingConfig"].(map[string]any); ok {
				if m, ok := fcc["mode"].(string); ok {
					fcMode = m
				}
			}
		}
		if rawTools, ok := req["tools"].([]any); ok && fcMode != "NONE" {
			for _, rt := range rawTools {
				toolGroup, ok := rt.(map[string]any)
				if !ok {
					continue
				}
				fns, ok := toolGroup["functionDeclarations"].([]any)
				if !ok {
					continue
				}
				for _, fn := range fns {
					fnMap, ok := fn.(map[string]any)
					if !ok {
						continue
					}
					td := tools.ToolDef{
						Name:        toString(fnMap["name"]),
						Description: toString(fnMap["description"]),
						Parameters:  fnMap["parameters"],
					}
					if td.Parameters == nil {
						td.Parameters = fnMap["parametersJsonSchema"]
					}
					toolDefs = append(toolDefs, td)
				}
			}
		}

		prompt, images := tools.GoogleContentsToPrompt(contents, systemText, toolDefs, fcMode)
		if prompt == "" {
			writeJSON(w, http.StatusBadRequest, errorObj("empty content"))
			return
		}

		fileRefs := s.uploadImages(r.Context(), images)

		hasTools := len(toolDefs) > 0 && fcMode != "NONE"

		if stream && !hasTools {
			s.streamGoogleGenerate(w, r.Context(), resolvedName, modelID, thinkMode, extra, prompt, fileRefs)
			return
		}

		text, err := s.gemini.Generate(r.Context(), prompt, modelID, thinkMode, fileRefs, extra)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, errorObj("upstream error: "+err.Error()))
			return
		}

		var responseParts []map[string]any
		if hasTools && text != "" {
			cleanText, funcCalls := tools.ParseGoogleFunctionCalls(text)
			if len(funcCalls) > 0 {
				if cleanText != "" {
					responseParts = append(responseParts, map[string]any{"text": cleanText})
				}
				for _, fc := range funcCalls {
					responseParts = append(responseParts, map[string]any{
						"functionCall": map[string]any{
							"name": fc.Name,
							"args": fc.Args,
						},
					})
				}
			} else {
				responseParts = append(responseParts, map[string]any{"text": text})
			}
		} else {
			responseText := text
			if responseText == "" {
				responseText = "I apologize, but I was unable to generate a response. Please try again."
			}
			responseParts = append(responseParts, map[string]any{"text": responseText})
		}

		candidate := map[string]any{
			"content": map[string]any{
				"parts": responseParts,
				"role":  "model",
			},
			"finishReason": "STOP",
			"index":        0,
		}
		usage := map[string]int{
			"promptTokenCount":     usageTokens(prompt),
			"candidatesTokenCount": usageTokens(text),
			"totalTokenCount":     usageTokens(prompt + text),
		}
		responseObj := map[string]any{
			"candidates":    []map[string]any{candidate},
			"usageMetadata": usage,
			"modelVersion":  resolvedName,
		}

		if stream {
			startSSE(w)
			_ = writeSSE(w, responseObj)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		} else {
			writeJSON(w, http.StatusOK, responseObj)
		}
	}
}

func (s *Server) streamGoogleGenerate(w http.ResponseWriter, ctx context.Context, modelName string, modelID, thinkMode int, extra map[int]int, prompt string, fileRefs []string) {
	ch := s.gemini.GenerateStream(ctx, prompt, modelID, thinkMode, fileRefs, extra)

	startSSE(w)
	flusher, ok := w.(http.Flusher)
	if !ok {
		flusher = nopFlusher{}
	}

	fullText := ""
	for evt := range ch {
		if evt.Err != nil {
			return
		}
		if evt.Delta == "" {
			continue
		}
		fullText += evt.Delta
		chunk := map[string]any{
			"candidates": []map[string]any{
				{
					"content": map[string]any{
						"parts": []map[string]string{{"text": evt.Delta}},
						"role":  "model",
					},
					"index": 0,
				},
			},
			"modelVersion": modelName,
		}
		if err := writeSSE(w, chunk); err != nil {
			return
		}
		flusher.Flush()
	}

	finalChunk := map[string]any{
		"candidates": []map[string]any{
			{"finishReason": "STOP", "index": 0},
		},
		"usageMetadata": map[string]int{
			"promptTokenCount":     usageTokens(prompt),
			"candidatesTokenCount": usageTokens(fullText),
			"totalTokenCount":      usageTokens(prompt + fullText),
		},
		"modelVersion": modelName,
	}
	_ = writeSSE(w, finalChunk)
	flusher.Flush()
}

func toString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}


