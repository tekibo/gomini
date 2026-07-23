package tools

import (
	"fmt"
	"strings"
)

func buildToolChoiceInstruction(toolChoice any, _ []ToolDef) string {
	switch tc := toolChoice.(type) {
	case string:
		switch tc {
		case "none":
			return "\n\nIMPORTANT: Do NOT call any tools. Respond with text only."
		case "required":
			return "\n\nIMPORTANT: You MUST call at least one tool. Do not respond with text only."
		case "auto":
			return ""
		}
	case map[string]any:
		if m, ok := tc["function"].(map[string]any); ok {
			if name, ok := m["name"].(string); ok && name != "" {
				return fmt.Sprintf(`\n\nIMPORTANT: You MUST call the tool "%s". Do not call other tools.`, name)
			}
		}
	}
	return ""
}

func googleToolChoiceInstruction(mode string, allowedNames []string) string {
	switch mode {
	case "NONE":
		return "\n\nIMPORTANT: Do NOT call any tools. Respond with text only."
	case "ANY":
		if len(allowedNames) > 0 {
			names := make([]string, len(allowedNames))
			for i, n := range allowedNames {
				names[i] = `"` + n + `"`
			}
			return "\n\nIMPORTANT: You MUST call one of these tools: " + strings.Join(names, ", ") + ". Do not respond with text only."
		}
		return "\n\nIMPORTANT: You MUST call at least one tool. Do not respond with text only."
	}
	return ""
}

func buildToolPrompt(toolSpec string) string {
	return "# Tool Use\n\n" +
		"You can call the following tools to help accomplish tasks. " +
		"These tools connect to the user's local environment and will execute when called.\n\n" +
		"Call format (use this exact format):\n" +
		"```function_call\n" +
		`{"name": "<tool_name>", "args": {<arguments>}}` + "\n" +
		"```\n\n" +
		"When calling tools:\n" +
		"- Output ONLY the function_call block(s), nothing else\n" +
		"- You may call multiple tools with multiple blocks\n" +
		"- After receiving a [Tool result for ...], use that data to answer the user\n\n" +
		"Available tools:\n" + toolSpec
}
