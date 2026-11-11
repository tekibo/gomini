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
