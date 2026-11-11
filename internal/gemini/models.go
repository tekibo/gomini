// Package gemini implements the Gemini StreamGenerate protocol client,
// model definitions, and authentication helpers.
package gemini

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// ModelConfig defines mode, thinking depth, variant, and routing ticket for a Gemini model.
type ModelConfig struct {
	Mode    int
	Think   int
	Variant int
	Ticket  string
	Levels  bool
	Extra   map[int]int
	Desc    string
}

var (
	// ReasoningEffortMap maps reasoning effort strings to (think, variant) pairs.
	ReasoningEffortMap = map[string][2]int{
		"minimal": {4, 1},
		"low":     {4, 1},
		"medium":  {2, 4},
		"high":    {0, 2},
		"xhigh":   {0, 2},
		"max":     {0, 2},
	}
	// ThinkToVariant maps thinking level (0, 2, 4) to ticket variant (2, 4, 1).
	ThinkToVariant = map[int]int{
		0: 2,
		2: 4,
		4: 1,
	}
	reSuffix = regexp.MustCompile(`-(low|medium|high)$`)
)

// Models is the catalog of known Gemini models and aliases.
var Models = map[string]ModelConfig{
	"gemini-4-argon":        {Mode: 8, Think: 2, Variant: 4, Ticket: "95a4c0abcba24364", Levels: true, Desc: "Gemini 4 Argon (Ultra web routing, medium thinking default)"},
	"gemini-4-argon-low":    {Mode: 8, Think: 4, Variant: 1, Ticket: "95a4c0abcba24364", Levels: false, Desc: "Gemini 4 Argon (low thinking)"},
	"gemini-4-argon-medium": {Mode: 8, Think: 2, Variant: 4, Ticket: "95a4c0abcba24364", Levels: false, Desc: "Gemini 4 Argon (medium thinking)"},
	"gemini-4-argon-high":   {Mode: 8, Think: 0, Variant: 2, Ticket: "95a4c0abcba24364", Levels: false, Desc: "Gemini 4 Argon (high thinking)"},

	"gemini-3.8-flash":               {Mode: 1, Think: 4, Variant: 1, Ticket: "56fdd199312815e2", Desc: "Fast all-around model (Flash family; serves 3.6/3.8 Flash per account)"},
	"gemini-3.8-flash-thinking":      {Mode: 2, Think: 0, Variant: 2, Ticket: "56fdd199312815e2", Desc: "Gemini 3.8 Flash with deep thinking (~20k chars)"},
	"gemini-3.8-flash-thinking-lite": {Mode: 5, Think: 0, Variant: 1, Ticket: "56fdd199312815e2", Desc: "Gemini 3.8 Flash with adaptive thinking depth"},
	"gemini-3.8-flash-high":          {Mode: 2, Think: 0, Variant: 2, Ticket: "56fdd199312815e2", Desc: "Gemini 3.8 Flash with deep thinking (high thinking)"},

	"gemini-3.7-flash":          {Mode: 1, Think: 4, Variant: 1, Desc: "Latest all-around model (Gemini 3.7 Flash)"},
	"gemini-3.7-flash-thinking": {Mode: 2, Think: 0, Variant: 2, Desc: "Deep thinking mode, longest output (~20k chars)"},
	"gemini-3.7-flash-high":     {Mode: 2, Think: 0, Variant: 2, Desc: "Deep thinking mode, longest output (~20k chars)"},

	"gemini-3.6-flash":          {Mode: 1, Think: 4, Variant: 1, Desc: "All-around model (Gemini 3.6 Flash)"},
	"gemini-3.6-flash-thinking": {Mode: 2, Think: 0, Variant: 2, Desc: "Deep thinking mode, longest output (~20k chars)"},
	"gemini-3.6-flash-high":     {Mode: 2, Think: 0, Variant: 2, Desc: "Deep thinking mode, longest output (~20k chars)"},

	"gemini-3.5-flash":               {Mode: 1, Think: 4, Variant: 1, Desc: "Alias for gemini-3.6-flash (backend upgraded)"},
	"gemini-3.5-flash-thinking":      {Mode: 2, Think: 0, Variant: 2, Desc: "Deep thinking mode, longest output (~20k chars)"},
	"gemini-3.5-flash-high":          {Mode: 2, Think: 0, Variant: 2, Desc: "Deep thinking mode, longest output (~20k chars)"},
	"gemini-3.5-flash-thinking-lite": {Mode: 5, Think: 0, Variant: 1, Desc: "Dynamic thinking with adaptive depth"},

	"gemini-3.1-pro":          {Mode: 3, Think: 4, Variant: 1, Ticket: "e6fa609c3fa255c0", Levels: true, Desc: "Pro model (requires cookie for real routing)"},
	"gemini-3.1-pro-low":      {Mode: 3, Think: 4, Variant: 1, Ticket: "e6fa609c3fa255c0", Levels: false, Desc: "Pro model (low thinking)"},
	"gemini-3.1-pro-medium":   {Mode: 3, Think: 2, Variant: 4, Ticket: "e6fa609c3fa255c0", Levels: false, Desc: "Pro model (medium thinking)"},
	"gemini-3.1-pro-high":     {Mode: 3, Think: 0, Variant: 2, Ticket: "e6fa609c3fa255c0", Levels: false, Desc: "Pro model (high thinking)"},
	"gemini-3.1-pro-enhanced": {Mode: 3, Think: 4, Variant: 1, Ticket: "e6fa609c3fa255c0", Extra: map[int]int{31: 2, 80: 3}, Desc: "Pro with enhanced output (experimental)"},

	"gemini-auto":       {Mode: 4, Think: 4, Desc: "Auto model selection"},
	"gemini-flash-lite": {Mode: 6, Think: 4, Variant: 1, Ticket: "8c46e95b1a07cecc", Desc: "Lightweight fast model"},
}

// BuildModelTicket constructs the X-Goog-Ext-525001261-Jspb JSPB array header value for model routing.
func BuildModelTicket(ticket string, mode, variant int) string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	u := fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
	arr := []any{
		1, nil, nil, nil, ticket, nil, nil, 0,
		[]int{4, 5, 6, 8, 4, 5, 6, 8},
		nil, nil, 2, nil, nil,
		mode, variant, u,
	}
	bytes, _ := json.Marshal(arr)
	return string(bytes)
}

func normalizeModelName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, ":", "-")
	name = strings.ReplaceAll(name, " (", "-")
	name = strings.ReplaceAll(name, "(", "-")
	name = strings.ReplaceAll(name, ")", "")
	name = strings.ReplaceAll(name, " ", "-")
	if !strings.HasPrefix(name, "gemini-") && (strings.HasPrefix(name, "3.") || strings.HasPrefix(name, "4-") || strings.HasPrefix(name, "flash") || strings.HasPrefix(name, "auto")) {
		name = "gemini-" + name
	}
	return name
}

// ResolveOptions contains parameters for resolving model names and generating model ticket headers.
type ResolveOptions struct {
	DefaultModel    string
	ReasoningEffort string
	ModelTickets    map[string]string
	SendModelTicket bool
}

// ResolveModel maps a requested model name, thinking level, and tickets to runtime generation parameters.
func ResolveModel(name string, opts ResolveOptions) (resolvedName string, mode, think int, extra map[int]int, ticketHeader string, err error) {
	defaultName := opts.DefaultModel
	if defaultName == "" || Models[defaultName].Desc == "" {
		defaultName = "gemini-3.6-flash"
	}

	name = strings.TrimSpace(name)
	if name == "" {
		name = defaultName
	}

	thinkOverride := -1
	variantOverride := -1

	if idx := strings.LastIndex(name, "@think="); idx >= 0 {
		thinkStr := name[idx+len("@think="):]
		name = name[:idx]
		n, parseErr := strconv.Atoi(thinkStr)
		if parseErr != nil {
			return "", 0, 0, nil, "", fmt.Errorf("invalid think level: %s", thinkStr)
		}
		thinkOverride = n
	}

	normalized := normalizeModelName(name)
	cfg, ok := Models[normalized]
	if !ok {
		cfg, ok = Models[name]
	}
	if ok {
		name = normalized
	} else {
		name = defaultName
		cfg = Models[defaultName]
	}

	if thinkOverride >= 0 {
		if cfg.Levels || cfg.Ticket != "" {
			if v, has := ThinkToVariant[thinkOverride]; has {
				variantOverride = v
			}
		}
	} else if opts.ReasoningEffort != "" && cfg.Levels {
		effortKey := strings.ToLower(strings.TrimSpace(opts.ReasoningEffort))
		if pair, has := ReasoningEffortMap[effortKey]; has {
			thinkOverride = pair[0]
			variantOverride = pair[1]
		}
	}

	think = cfg.Think
	if thinkOverride >= 0 {
		think = thinkOverride
	}

	effectiveVariant := cfg.Variant
	if variantOverride > 0 {
		effectiveVariant = variantOverride
	}
	if effectiveVariant == 0 {
		effectiveVariant = 1
	}

	extra = make(map[int]int)
	for k, v := range cfg.Extra {
		extra[k] = v
	}

	if opts.SendModelTicket {
		baseName := reSuffix.ReplaceAllString(name, "")
		ticket := ""
		if opts.ModelTickets != nil {
			if t, has := opts.ModelTickets[name]; has && t != "" {
				ticket = t
			} else if t, has := opts.ModelTickets[baseName]; has && t != "" {
				ticket = t
			}
		}
		if ticket == "" {
			ticket = cfg.Ticket
		}
		if ticket != "" {
			ticketHeader = BuildModelTicket(ticket, cfg.Mode, effectiveVariant)
		}
	}

	return name, cfg.Mode, think, extra, ticketHeader, nil
}
