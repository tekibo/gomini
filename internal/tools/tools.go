// Package tools provides message-to-prompt conversion and
// tool call parsing for OpenAI and Google API formats.
package tools

import (
	"crypto/rand"
	"encoding/hex"
)

func randHex(n int) string {
	b := make([]byte, n/2+1)
	rand.Read(b)
	return hex.EncodeToString(b)[:n]
}

type Message struct {
	Role       string     `json:"role"`
	Content    any        `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	Name       string     `json:"name,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type ContentPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL any    `json:"image_url,omitempty"`
	Image    any    `json:"image,omitempty"`
}

type ImageData struct {
	Data     []byte
	MIMEType string
}

type ToolDef struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"`
}

type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type FunctionCall struct {
	Name string `json:"name"`
	Args any    `json:"args"`
}

type GoogleContent struct {
	Role  string `json:"role"`
	Parts []any  `json:"parts"`
}
