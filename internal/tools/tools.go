// Package tools provides message-to-prompt conversion and
// tool call parsing for OpenAI and Google API formats.
package tools

import (
	"crypto/rand"
	"encoding/hex"
	"regexp"
	"strings"
)

// ToolCallMarker is the markdown code block fence marker used for tool call blocks.
const ToolCallMarker = "```tool_call"

// EchoLabels is the list of conversational prefix labels that models may hallucinate.
var (
	EchoLabels = []string{
		"[Assistant]:",
		"Assistant:",
		"[Model]:",
		"Model:",
		"[AI]:",
		"AI:",
		"[Response]:",
		"Response:",
	}

	failurePatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)i'?m having a hard time fulfilling your request`),
		regexp.MustCompile(`(?i)sorry,? something went wrong`),
		regexp.MustCompile(`(?i)something went wrong\.? please try`),
		regexp.MustCompile(`(?i)i seem to be encountering an error`),
		regexp.MustCompile(`(?i)i encountered an error doing what you asked`),
		regexp.MustCompile(`(?i)an error occurred\.? please try`),
		regexp.MustCompile(`(?i)i'?m unable to (?:process|complete) (?:that|this|your) request right now`),
	}
	refusalPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)i'?m (?:just )?a language model,? (?:and|so) (?:i )?can'?t help`),
		regexp.MustCompile(`(?i)i'?m not able to help with that,? as i'?m only a language model`),
		regexp.MustCompile(`(?i)i'?m a text-based ai,? (?:and|so) that is outside of my capabilities`),
		regexp.MustCompile(`(?i)i can'?t assist (?:you )?with that,? as i'?m only a language model`),
	}
	leadFailurePatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bi (?:cannot|can'?t|am unable to|'?m unable to|am not able to|'?m not able to) (?:execute|make|call|run|perform|use|access)(?: any| the)?(?: additional| more| further| file)? (?:tool|function)`),
		regexp.MustCompile(`(?i)\b(?:tool|function)[ -]call(?:ing)?s? (?:limit (?:has been |was )?reached|are exhausted|have been exhausted)`),
		regexp.MustCompile(`(?i)\bmaximum (?:number of )?(?:tool calls|tool call limit|call limit|limit) (?:has been |was )?reached`),
		regexp.MustCompile(`(?i)\bi(?: am|'m)? (?:unable|not able|cannot|can'?t)\b.{0,120}\b(?:tools?|tool calls?) (?:are|is) (?:currently |temporarily )?(?:unavailable|disabled|not available)`),
		regexp.MustCompile(`(?i)\b(?:tools?|tool calls?) (?:are|is) (?:currently |temporarily )?(?:unavailable|disabled|not available)\b.{0,120}\bi(?: am|'m)? (?:unable|not able|cannot|can'?t)\b`),
		regexp.MustCompile(`(?i)\bi(?: am|'m)? (?:unable|not able|cannot|can'?t) to (?:run|use|access|call|execute)\b[^.]{0,60}\btools?\b`),
		regexp.MustCompile(`(?i)\bwithout (?:tool|file) access\b`),
	}
	tutorialPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\byou can (?:use|make|apply|follow|add|update)\b`),
		regexp.MustCompile(`(?i)\bhere (?:are|is) (?:the )?(?:changes|updates|code|steps)\b`),
		regexp.MustCompile(`(?i)\bfollow(?:ing)? (?:these )?steps\b`),
		regexp.MustCompile(`(?i)\b(?:add|include|use) the following\b`),
		regexp.MustCompile(`(?i)\bto (?:complete|fulfill|accomplish) (?:your|this|the) request\b`),
	}
)

// EchoPrefixLen returns the length of any echoed prompt label at the start of text.
func EchoPrefixLen(text string) int {
	trimmed := strings.TrimLeft(text, " \t\r\n")
	leadSpaces := len(text) - len(trimmed)
	for _, label := range EchoLabels {
		if strings.HasPrefix(strings.ToLower(trimmed), strings.ToLower(label)) {
			rest := trimmed[len(label):]
			restTrimmed := strings.TrimLeft(rest, " \t\r\n")
			return leadSpaces + len(label) + (len(rest) - len(restTrimmed))
		}
	}
	return 0
}

// ClassifyCanned detects canned Gemini errors or refusal messages.
func ClassifyCanned(text string) string {
	if text == "" {
		return ""
	}
	t := strings.ToLower(strings.TrimSpace(text))
	t = strings.ReplaceAll(t, "\u2019", "'")

	lead := t
	if len(lead) > 200 {
		lead = lead[:200]
	}
	for _, p := range leadFailurePatterns {
		if p.MatchString(lead) {
			return "failure"
		}
	}

	maxCanned := t
	if len(maxCanned) > 300 {
		maxCanned = maxCanned[:300]
	}
	for _, p := range failurePatterns {
		if p.MatchString(maxCanned) {
			return "failure"
		}
	}
	for _, p := range refusalPatterns {
		if p.MatchString(maxCanned) {
			return "refusal"
		}
	}
	return ""
}

// TutorialPhrasing checks if text explains steps instead of invoking tools.
func TutorialPhrasing(text string) bool {
	t := strings.ToLower(text)
	if len(t) > 600 {
		t = t[:600]
	}
	for _, p := range tutorialPatterns {
		if p.MatchString(t) {
			return true
		}
	}
	return false
}

// LooksLikeTutorial returns true if text contains code fences and tutorial phrasing instead of tool calls.
func LooksLikeTutorial(text string) bool {
	return strings.Contains(text, "```") && TutorialPhrasing(text)
}

func randHex(n int) string {
	b := make([]byte, n/2+1)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)[:n]
}

// Message represents a conversation turn in OpenAI chat format.
type Message struct {
	Role       string     `json:"role"`
	Content    any        `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	Name       string     `json:"name,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ContentPart represents a text or image part of a message content array.
type ContentPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL any    `json:"image_url,omitempty"`
	Image    any    `json:"image,omitempty"`
}

// ImageData contains decoded bytes or URL for multimodal image inputs.
type ImageData struct {
	Data     []byte
	URL      string
	MIMEType string
}

// ToolDef represents a callable function tool definition.
type ToolDef struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"`
}

// ToolCall represents an invoked tool call emitted by the model.
type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// FunctionCall represents a Google Gemini function call.
type FunctionCall struct {
	Name string `json:"name"`
	Args any    `json:"args"`
}

// GoogleContent represents a conversation content turn in Google's native API format.
type GoogleContent struct {
	Role  string `json:"role"`
	Parts []any  `json:"parts"`
}
