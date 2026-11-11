package tools

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

const promptMaxBytes = 60000

var dataURLRe = regexp.MustCompile(`(?s)^data:([^;,]+)?(;base64)?,(.*)$`)

func decodeDataURL(rawURL string) ([]byte, string, bool) {
	m := dataURLRe.FindStringSubmatch(rawURL)
	if m == nil {
		return nil, "", false
	}
	mime := m[1]
	if mime == "" {
		mime = "image/png"
	}
	isBase64 := m[2] != ""
	dataPart := m[3]
	if isBase64 {
		decoded, err := base64.StdEncoding.DecodeString(dataPart)
		if err != nil {
			return nil, "", false
		}
		return decoded, mime, true
	}
	unescaped, err := url.QueryUnescape(dataPart)
	if err != nil {
		return nil, "", false
	}
	return []byte(unescaped), mime, true
}

func imageFromURL(rawURL, defaultMIME string) (ImageData, bool) {
	if rawURL == "" {
		return ImageData{}, false
	}
	if strings.HasPrefix(rawURL, "data:") {
		data, mime, ok := decodeDataURL(rawURL)
		if !ok {
			return ImageData{}, false
		}
		return ImageData{Data: data, MIMEType: mime}, true
	}
	mime := defaultMIME
	if mime == "" {
		mime = "image/png"
	}
	return ImageData{URL: rawURL, MIMEType: mime}, true
}

func imageFromPart(item any) (ImageData, bool) {
	m, ok := item.(map[string]any)
	if !ok {
		b, err := json.Marshal(item)
		if err != nil {
			return ImageData{}, false
		}
		if err := json.Unmarshal(b, &m); err != nil {
			return ImageData{}, false
		}
	}

	partType, _ := m["type"].(string)
	if partType == "image_url" {
		iu := m["image_url"]
		if iuMap, ok := iu.(map[string]any); ok {
			rawURL, _ := iuMap["url"].(string)
			mime, _ := iuMap["mime_type"].(string)
			return imageFromURL(rawURL, mime)
		}
		if rawURL, ok := iu.(string); ok {
			return imageFromURL(rawURL, "")
		}
	}

	if partType == "input_image" || partType == "image" {
		iu := m["image_url"]
		if iu == nil {
			iu = m["url"]
		}
		if iuMap, ok := iu.(map[string]any); ok {
			rawURL, _ := iuMap["url"].(string)
			mime, _ := iuMap["mime_type"].(string)
			return imageFromURL(rawURL, mime)
		}
		if rawURL, ok := iu.(string); ok && rawURL != "" {
			mime, _ := m["mime_type"].(string)
			return imageFromURL(rawURL, mime)
		}

		rawBytesVal := m["data"]
		if rawBytesVal == nil {
			rawBytesVal = m["base64"]
		}
		if s, ok := rawBytesVal.(string); ok && s != "" {
			mime, _ := m["mime_type"].(string)
			if mime == "" {
				mime, _ = m["media_type"].(string)
			}
			if mime == "" {
				mime = "image/png"
			}
			if strings.HasPrefix(s, "data:") {
				return imageFromURL(s, mime)
			}
			data, err := base64.StdEncoding.DecodeString(s)
			if err != nil {
				return ImageData{}, false
			}
			return ImageData{Data: data, MIMEType: mime}, true
		}
	}

	return ImageData{}, false
}

// MessagesToPrompt converts an array of OpenAI messages, tools, and tool_choice into a single prompt string and image inputs.
func MessagesToPrompt(messages []Message, tools []ToolDef, toolChoice any) (string, []ImageData) {
	var parts []string
	var images []ImageData

	if len(tools) > 0 {
		tcStr, _ := json.MarshalIndent(tools, "", "  ")
		if len(tcStr) > promptMaxBytes/2 {
			type slimTool struct {
				Name        string `json:"name"`
				Description string `json:"description"`
			}
			slim := make([]slimTool, len(tools))
			for i, t := range tools {
				slim[i] = slimTool{Name: t.Name, Description: t.Description}
			}
			tcStr, _ = json.MarshalIndent(slim, "", "  ")
		}
		constraint := buildToolChoiceInstruction(toolChoice, tools)
		parts = append(parts,
			"# Tool Use\n\n"+
				"You can call the following tools. Call format:\n"+
				"```tool_call\n{\"name\": \"func_name\", \"arguments\": {...}}\n```\n"+
				"When calling tools, output ONLY the tool_call block(s).\n\n"+
				"Available tools:\n"+string(tcStr)+
				constraint,
		)
	}

	for _, msg := range messages {
		role := msg.Role
		content := ""

		switch c := msg.Content.(type) {
		case string:
			content = c
		case []any:
			var texts []string
			for _, item := range c {
				part, err := toContentPart(item)
				if err != nil {
					continue
				}
				switch part.Type {
				case "text", "input_text", "output_text":
					texts = append(texts, part.Text)
				default:
					if img, ok := imageFromPart(item); ok {
						images = append(images, img)
						texts = append(texts, "[Image attached]")
					}
				}
			}
			content = strings.Join(texts, " ")
		case nil:
			content = ""
		}

		switch role {
		case "system":
			if content != "" {
				parts = append(parts, "[System instruction]: "+content)
			}
		case "assistant":
			var tcStrs []string
			for _, tc := range msg.ToolCalls {
				tcStrs = append(tcStrs, fmt.Sprintf(
					"```tool_call\n{\"name\": %q, \"arguments\": %s}\n```",
					tc.Function.Name, tc.Function.Arguments,
				))
			}
			if content != "" || len(tcStrs) > 0 {
				line := "[Assistant]: " + content
				if len(tcStrs) > 0 {
					line += "\n" + strings.Join(tcStrs, "\n")
				}
				parts = append(parts, line)
			}
		case "tool":
			parts = append(parts, fmt.Sprintf("[Tool result for %s]: %s", msg.Name, content))
		default:
			if content != "" {
				parts = append(parts, content)
			}
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

func toContentPart(v any) (ContentPart, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return ContentPart{}, err
	}
	var cp ContentPart
	if err := json.Unmarshal(b, &cp); err != nil {
		return ContentPart{}, err
	}
	return cp, nil
}

var toolCallRe = regexp.MustCompile("(?s)```tool_call\\s*\n(.*?)(?:\n```|$)")

// FindToolCallStart returns the index where a tool_call fence begins in text starting from offset start, or -1.
func FindToolCallStart(text string, start int) int {
	if start >= len(text) {
		return -1
	}
	sub := text[start:]
	idx := strings.Index(sub, ToolCallMarker)
	if idx == -1 {
		return -1
	}
	return start + idx
}

// ParseToolCalls extracts tool call fences from generated text and returns clean prose and parsed ToolCalls.
func ParseToolCalls(text string) (string, []ToolCall) {
	if skip := EchoPrefixLen(text); skip > 0 {
		text = text[skip:]
	}

	var toolCalls []ToolCall
	var cleanParts []string
	lastEnd := 0

	indices := toolCallRe.FindAllStringSubmatchIndex(text, -1)
	for _, m := range indices {
		cleanParts = append(cleanParts, text[lastEnd:m[0]])
		lastEnd = m[1]
		body := strings.TrimSpace(text[m[2]:m[3]])
		var data struct {
			Name      string `json:"name"`
			Arguments any    `json:"arguments"`
		}
		if err := json.Unmarshal([]byte(body), &data); err != nil || data.Name == "" {
			continue
		}
		argsStr := "{}"
		if data.Arguments != nil {
			if s, ok := data.Arguments.(string); ok {
				argsStr = s
			} else {
				b, _ := json.Marshal(data.Arguments)
				argsStr = string(b)
			}
		}
		toolCalls = append(toolCalls, ToolCall{
			ID:   "call_" + randHex(8),
			Type: "function",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{
				Name:      data.Name,
				Arguments: argsStr,
			},
		})
	}

	if len(toolCalls) == 0 {
		cleanParts = append(cleanParts, text[lastEnd:])
	}
	clean := strings.TrimSpace(strings.Join(cleanParts, ""))

	return clean, toolCalls
}
