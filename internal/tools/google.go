package tools

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

func GoogleContentsToPrompt(contents []GoogleContent, systemInstruction string, tools []ToolDef, toolChoice string) (string, []ImageData) {
	var parts []string
	var images []ImageData

	hasTools := len(tools) > 0 && toolChoice != "NONE"

	if systemInstruction != "" {
		if hasTools {
			tcStr, _ := json.MarshalIndent(tools, "", "  ")
			constraint := googleToolChoiceInstruction(toolChoice, nil)
			parts = append(parts, systemInstruction+"\n\n"+
				buildToolPrompt(string(tcStr))+constraint)
		} else {
			parts = append(parts, systemInstruction)
		}
	} else if hasTools {
		tcStr, _ := json.MarshalIndent(tools, "", "  ")
		constraint := googleToolChoiceInstruction(toolChoice, nil)
		parts = append(parts, buildToolPrompt(string(tcStr))+constraint)
	}

	for _, content := range contents {
		role := content.Role
		var msgParts []string
		for _, p := range content.Parts {
			b, _ := json.Marshal(p)
			var part struct {
				Text       string `json:"text"`
				InlineData *struct {
					Data     string `json:"data"`
					MIMEType string `json:"mimeType"`
				} `json:"inlineData,omitempty"`
				FunctionCall *struct {
					Name string `json:"name"`
					Args any    `json:"args"`
				} `json:"functionCall,omitempty"`
				FunctionResponse *struct {
					Name     string `json:"name"`
					Response any    `json:"response"`
				} `json:"functionResponse,omitempty"`
			}
			if err := json.Unmarshal(b, &part); err != nil {
				continue
			}

			if part.Text != "" {
				msgParts = append(msgParts, part.Text)
			} else if part.InlineData != nil {
				b, err := json.Marshal(part.InlineData)
				if err == nil {
					var raw struct {
						Data     string `json:"data"`
						MIMEType string `json:"mimeType"`
					}
					if err := json.Unmarshal(b, &raw); err != nil {
						continue
					}
					decoded, err := base64.StdEncoding.DecodeString(raw.Data)
					if err == nil {
						images = append(images, ImageData{Data: decoded, MIMEType: raw.MIMEType})
					}
				}
			} else if part.FunctionCall != nil {
				argsJSON, _ := json.Marshal(part.FunctionCall.Args)
				msgParts = append(msgParts, fmt.Sprintf(
					"```function_call\n%s\n```",
					string(argsJSON),
				))
			} else if part.FunctionResponse != nil {
				respJSON, _ := json.Marshal(part.FunctionResponse.Response)
				msgParts = append(msgParts, fmt.Sprintf(
					"[Tool result for %s]: %s",
					part.FunctionResponse.Name, string(respJSON),
				))
			}
		}

		text := strings.Join(msgParts, "\n")
		if role == "model" {
			parts = append(parts, "[Assistant]: "+text)
		} else if text != "" {
			parts = append(parts, text)
		}
	}

	var filtered []string
	for _, p := range parts {
		if p != "" {
			filtered = append(filtered, p)
		}
	}
	return strings.Join(filtered, "\n\n"), images
}

var funcCallRe1 = regexp.MustCompile("(?s)```function_call\\s*\n(.*?)\n```")
var funcCallRe2 = regexp.MustCompile("(?:^|\n)function_call\\s*\n(\\{[^`]*?\\})")

func ParseGoogleFunctionCalls(text string) (string, []FunctionCall) {
	var funcCalls []FunctionCall
	clean := text

	for _, re := range []*regexp.Regexp{funcCallRe1, funcCallRe2} {
		for _, m := range re.FindAllStringSubmatch(clean, -1) {
			var data struct {
				Name string `json:"name"`
				Args any    `json:"args"`
			}
			if err := json.Unmarshal([]byte(strings.TrimSpace(m[1])), &data); err != nil || data.Name == "" {
				continue
			}
			funcCalls = append(funcCalls, FunctionCall{Name: data.Name, Args: data.Args})
		}
		clean = re.ReplaceAllString(clean, "")
	}
	clean = strings.TrimSpace(clean)

	if len(funcCalls) == 0 && strings.HasPrefix(clean, "{") {
		var data struct {
			Name string `json:"name"`
			Args any    `json:"args"`
		}
		if err := json.Unmarshal([]byte(clean), &data); err == nil && data.Name != "" {
			funcCalls = append(funcCalls, FunctionCall{Name: data.Name, Args: data.Args})
			clean = ""
		}
	}

	return clean, funcCalls
}
