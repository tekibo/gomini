package tools

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestMessagesToPromptExtractsOpenAIDataURL(t *testing.T) {
	b64 := base64.StdEncoding.EncodeToString([]byte("fake png"))
	msgs := []Message{
		{
			Role: "user",
			Content: []any{
				map[string]any{"type": "text", "text": "Describe"},
				map[string]any{
					"type": "image_url",
					"image_url": map[string]any{
						"url": "data:image/png;base64," + b64,
					},
				},
			},
		},
	}

	prompt, images := MessagesToPrompt(msgs, nil, "auto")
	if prompt != "Describe [Image attached]" {
		t.Errorf("unexpected prompt: %q", prompt)
	}
	if len(images) != 1 {
		t.Fatalf("expected 1 image, got %d", len(images))
	}
	if string(images[0].Data) != "fake png" {
		t.Errorf("expected data 'fake png', got %q", string(images[0].Data))
	}
	if images[0].MIMEType != "image/png" {
		t.Errorf("expected mime image/png, got %q", images[0].MIMEType)
	}
}

func TestMessagesToPromptExtractsResponsesInputImageURL(t *testing.T) {
	msgs := []Message{
		{
			Role: "user",
			Content: []any{
				map[string]any{"type": "input_text", "text": "Describe"},
				map[string]any{"type": "input_image", "image_url": "https://example.com/image.png"},
			},
		},
	}

	prompt, images := MessagesToPrompt(msgs, nil, "auto")
	if prompt != "Describe [Image attached]" {
		t.Errorf("unexpected prompt: %q", prompt)
	}
	if len(images) != 1 {
		t.Fatalf("expected 1 image, got %d", len(images))
	}
	if images[0].URL != "https://example.com/image.png" {
		t.Errorf("expected URL https://example.com/image.png, got %q", images[0].URL)
	}
}

func TestMessagesToPromptIgnoresMalformedImageURL(t *testing.T) {
	msgs := []Message{
		{
			Role: "user",
			Content: []any{
				map[string]any{"type": "text", "text": "Describe"},
				map[string]any{
					"type": "image_url",
					"image_url": map[string]any{
						"url": "data:image/png;base64,%%invalid%%",
					},
				},
			},
		},
	}

	prompt, images := MessagesToPrompt(msgs, nil, "auto")
	if prompt != "Describe" {
		t.Errorf("unexpected prompt: %q", prompt)
	}
	if len(images) != 0 {
		t.Errorf("expected 0 images, got %d", len(images))
	}
}

func TestToolsBlockTruncationWhenLarge(t *testing.T) {
	var bigTools []ToolDef
	for i := 0; i < 200; i++ {
		bigTools = append(bigTools, ToolDef{
			Name:        "tool_" + string(rune('a'+(i%26))),
			Description: "description for tool with lots of schema properties",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"nested_data": map[string]any{
						"type":        "string",
						"description": "very long description that takes up significant bytes in schema block",
					},
				},
			},
		})
	}

	prompt, _ := MessagesToPrompt([]Message{{Role: "user", Content: "hello"}}, bigTools, "auto")
	if strings.Contains(prompt, "nested_data") {
		t.Errorf("expected parameters to be stripped when tool schemas exceed limit")
	}
	if !strings.Contains(prompt, "description for tool with lots of schema properties") {
		t.Errorf("expected tool descriptions to be retained")
	}
}

func TestClassifyCanned(t *testing.T) {
	if ClassifyCanned("I'm having a hard time fulfilling your request right now.") != "failure" {
		t.Errorf("expected 'failure' for hard time fulfilling request")
	}
	if ClassifyCanned("Sorry, something went wrong.") != "failure" {
		t.Errorf("expected 'failure' for something went wrong")
	}
	if ClassifyCanned("I cannot execute tools directly because the tool call limit has been reached.") != "failure" {
		t.Errorf("expected 'failure' for tool call limit reached")
	}
	if ClassifyCanned("I'm just a language model, and I can't help with that.") != "refusal" {
		t.Errorf("expected 'refusal' for language model disclaimer")
	}
	if ClassifyCanned("Here is the answer to your question: 42.") != "" {
		t.Errorf("expected empty string for normal response")
	}
}

func TestParseToolCallsUnclosedAndEcho(t *testing.T) {
	raw := "[Assistant]: Creating file.\n```tool_call\n{\"name\": \"write_file\", \"arguments\": {\"path\": \"a.txt\"}}\n"
	clean, calls := ParseToolCalls(raw)
	if clean != "Creating file." {
		t.Errorf("expected clean 'Creating file.', got %q", clean)
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].Function.Name != "write_file" {
		t.Errorf("expected function write_file, got %s", calls[0].Function.Name)
	}
}
