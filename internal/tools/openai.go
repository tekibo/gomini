package tools

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

func MessagesToPrompt(messages []Message, tools []ToolDef, toolChoice any) (string, []ImageData) {
	var parts []string
	var images []ImageData

	if len(tools) > 0 {
		tcStr, _ := json.MarshalIndent(tools, "", "  ")
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
				case "text", "input_text":
					texts = append(texts, part.Text)
				case "image_url", "image":
					texts = append(texts, "[Note: Image input not supported in this API. Please describe the image in text.]")
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

var toolCallRe = regexp.MustCompile("(?s)```tool_call\\s*\n(.*?)\n```")

func ParseToolCalls(text string) (string, []ToolCall) {
	var toolCalls []ToolCall
	var cleanParts []string
	lastEnd := 0

	for _, m := range toolCallRe.FindAllStringSubmatchIndex(text, -1) {
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
	cleanParts = append(cleanParts, text[lastEnd:])
	clean := strings.TrimSpace(strings.Join(cleanParts, ""))

	return clean, toolCalls
}
