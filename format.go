package main

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	cSession  = "\033[38;5;141m" // Lavender
	cModel    = "\033[38;5;210m" // Coral
	cModePlan = "\033[38;5;117m" // Sky
	cModeEdit = "\033[38;5;221m" // Warm Gold
	cModeAuto = "\033[38;5;212m" // Pink
	cModeAsk  = "\033[38;5;156m" // Lime
	cStatIdle = "\033[38;5;114m" // Mint
	cStatWork = "\033[38;5;208m" // Tangerine
	cStatTool = "\033[38;5;80m"  // Turquoise
	cStatRev  = "\033[38;5;176m" // Orchid
	cSub      = "\033[38;5;81m"  // Electric Cyan
	cTask     = "\033[38;5;229m" // Warm Cream

	green    = "\033[32m"
	yellow   = "\033[33m"
	red      = "\033[31m"
	bgGreen  = "\033[42m"
	bgYellow = "\033[43m"
	bgRed    = "\033[41m"
	bgTrack  = "\033[48;5;236m"
	fgInk    = "\033[38;5;16m"
	bold     = "\033[1m"
	dim      = "\033[2m"
	reset    = "\033[0m"
)

func formatEffort(effort string) string {
	switch strings.ToLower(effort) {
	case "low":
		return "Low"
	case "medium":
		return "Medium"
	case "high":
		return "High"
	case "none":
		return "None"
	default:
		r, size := utf8.DecodeRuneInString(effort)
		if r == utf8.RuneError {
			return effort
		}
		return string(unicode.ToUpper(r)) + effort[size:]
	}
}

func stripVendorPrefix(model string) string {
	if strings.HasPrefix(model, "Gemini ") {
		rest := model[len("Gemini "):]
		if len(rest) > 0 && rest[0] >= '0' && rest[0] <= '9' {
			if idx := strings.IndexByte(rest, ' '); idx > 0 {
				return rest[idx+1:]
			}
		}
		return rest
	}
	for _, pfx := range []string{"Claude ", "OpenAI "} {
		if strings.HasPrefix(model, pfx) {
			return model[len(pfx):]
		}
	}
	return model
}

func abbreviateEffort(model string) string {
	if !strings.HasSuffix(model, ")") {
		return model
	}
	openIdx := strings.LastIndexByte(model, '(')
	if openIdx < 0 || openIdx+2 >= len(model) {
		return model
	}
	inside := model[openIdx+1 : len(model)-1]
	r, _ := utf8.DecodeRuneInString(inside)
	if r == utf8.RuneError || !unicode.IsLetter(r) {
		return model
	}
	return model[:openIdx+1] + string(r) + ")"
}

func formatMode(raw string) string {
	switch strings.ToLower(raw) {
	case "plan":
		return cModePlan + "Plan" + reset
	case "accept-edits", "acceptedits", "edit":
		return cModeEdit + "Edits" + reset
	case "auto", "yolo":
		return cModeAuto + "Auto" + reset
	case "default", "ask":
		return cModeAsk + "Ask" + reset
	case "":
		return dim + "--" + reset
	default:
		if strings.HasPrefix(strings.ToLower(raw), "bypass") {
			return cModeAuto + "Auto" + reset
		}
		return cModePlan + raw + reset
	}
}

func formatStatus(raw string) string {
	switch strings.ToLower(raw) {
	case "idle", "ready":
		return cStatIdle + "Idle" + reset
	case "working", "thinking", "streaming", "busy":
		return cStatWork + "Working" + reset
	case "tool_use", "tool", "running", "executing":
		return cStatTool + "Tool" + reset
	case "reviewing", "waiting", "approval":
		return cStatRev + "Review" + reset
	case "":
		return dim + "--" + reset
	default:
		return cStatWork + raw + reset
	}
}

func parseSubagents(v any) (int, int) {
	switch sub := v.(type) {
	case []any:
		active := 0
		for _, item := range sub {
			st := strings.ToLower(firstStr(asMap(item)["status"]))
			if st == "running" || st == "working" || st == "in_progress" || st == "active" {
				active++
			}
		}
		return active, len(sub)
	case map[string]any:
		active := asInt(sub["active"])
		if active == 0 {
			active = asInt(sub["running"])
		}
		total := asInt(sub["total"])
		if total == 0 {
			total = asInt(sub["count"])
		}
		return active, total
	default:
		n := asInt(v)
		return n, n
	}
}

func truncateRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max-1]) + "…"
}

func truncatePlain(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max])
}

func visibleLen(s string) int {
	count := 0
	inEsc := false
	for i := 0; i < len(s); {
		if !inEsc && s[i] == '\033' && i+1 < len(s) && s[i+1] == '[' {
			inEsc = true
			i += 2
			continue
		}
		if inEsc {
			if (s[i] >= 'A' && s[i] <= 'Z') || (s[i] >= 'a' && s[i] <= 'z') {
				inEsc = false
			}
			i++
			continue
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		count++
		i += size
	}
	return count
}
