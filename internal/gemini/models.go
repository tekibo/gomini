// Package gemini implements the Gemini StreamGenerate protocol client,
// model definitions, and authentication helpers.
package gemini

import (
	"fmt"
	"strconv"
	"strings"
)

type ModelConfig struct {
	Mode  int
	Think int
	Extra map[int]int
	Desc  string
}

var Models = map[string]ModelConfig{
	"gemini-3.6-flash":              {Mode: 1, Think: 4, Desc: "Latest all-around model (Gemini 3.6 Flash)"},
	"gemini-3.5-flash":              {Mode: 1, Think: 4, Desc: "Alias for gemini-3.6-flash (backend upgraded)"},
	"gemini-3.5-flash-thinking":     {Mode: 2, Think: 0, Desc: "Deep thinking mode, longest output (~20k chars)"},
	"gemini-3.1-pro":               {Mode: 3, Think: 4, Desc: "Pro model (requires cookie for real routing)"},
	"gemini-3.1-pro-enhanced":      {Mode: 3, Think: 4, Extra: map[int]int{31: 2, 80: 3}, Desc: "Pro with enhanced output (experimental)"},
	"gemini-auto":                  {Mode: 4, Think: 4, Desc: "Auto model selection"},
	"gemini-3.5-flash-thinking-lite": {Mode: 5, Think: 0, Desc: "Dynamic thinking with adaptive depth"},
	"gemini-flash-lite":            {Mode: 6, Think: 4, Desc: "Lightweight fast model"},
}

func ResolveModel(name, defaultName string) (resolvedName string, mode, think int, extra map[int]int, err error) {
	thinkOverride := -1
	if idx := strings.LastIndex(name, "@think="); idx >= 0 {
		thinkStr := name[idx+len("@think="):]
		name = name[:idx]
		n, parseErr := strconv.Atoi(thinkStr)
		if parseErr != nil {
			return "", 0, 0, nil, fmt.Errorf("invalid think level: %s", thinkStr)
		}
		thinkOverride = n
	}

	cfg, ok := Models[name]
	if !ok {
		name = defaultName
		cfg = Models[defaultName]
	}

	think = cfg.Think
	if thinkOverride >= 0 {
		think = thinkOverride
	}
	return name, cfg.Mode, think, cfg.Extra, nil
}
