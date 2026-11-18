package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
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

	green  = "\033[32m"
	yellow = "\033[33m"
	red    = "\033[31m"
	dim    = "\033[2m"
	reset  = "\033[0m"
)

func main() {
	raw, _ := io.ReadAll(os.Stdin)
	home, _ := os.UserHomeDir()
	fmt.Println(renderStatusline(raw, home, detectTTYCols))
}

func renderStatusline(raw []byte, homeDir string, ttyWidthFn func() int) string {
	var data map[string]any
	if len(bytes.TrimSpace(raw)) > 0 {
		_ = json.Unmarshal(raw, &data)
	}
	if data == nil {
		data = map[string]any{}
	}

	cols := resolveCols(data, ttyWidthFn)
	sid := firstStr(data["conversation_id"], data["session_id"])
	payloadTitle := firstStr(data["conversation_title"], data["session_title"], data["title"])
	transcriptPath := firstStr(data["transcript_path"])
	artifactCount := asInt(data["artifact_count"])

	brainDir := resolveBrainDir(homeDir, sid, transcriptPath)

	// 1. Session Name
	sessionLabel := ""
	if isValidTitle(payloadTitle, sid) {
		sessionLabel = payloadTitle
	} else if sid != "" {
		sessionLabel = resolveSessionTitle(homeDir, sid)
	}

	segSession := dim + "--" + reset
	if sessionLabel != "" {
		segSession = cSession + truncateRunes(sessionLabel, 18) + reset
	} else if sid != "" {
		segSession = dim + truncatePlain(sid, 8) + reset
	}

	// 2. Model Variants
	mObj := asMap(data["model"])
	mID := firstStr(mObj["id"], data["model_id"])
	mName := firstStr(
		mObj["display_name"],
		mObj["label"],
		mObj["name"],
		mObj["model_name"],
		mObj["id"],
		data["model"],
		data["model_name"],
		data["selected_model"],
	)
	effort := firstStr(mObj["effort"], data["effort"])

	segModelFull := dim + "--" + reset
	segModelShort := segModelFull
	segModelMini := segModelFull
	if mName != "" {
		if effort != "" && !strings.Contains(strings.ToLower(mName), "("+strings.ToLower(effort)+")") {
			mName += " (" + formatEffort(effort) + ")"
		}
		segModelFull = cModel + mName + reset
		shortModel := stripVendorPrefix(mName)
		segModelShort = cModel + shortModel + reset
		segModelMini = cModel + abbreviateEffort(shortModel) + reset
	}

	// 3. Mode
	modeRaw := firstStr(data["cycle_mode"], data["permission_mode"], data["mode"])
	segMode := formatMode(modeRaw)

	// 4. Status
	aObj := asMap(data["agent_state"])
	statRaw := firstStr(aObj["state"], aObj["status"], data["agent_state"], data["status"], data["state"])
	segStatus := formatStatus(statRaw)

	// 5. Subagents
	subActive, subTotal := parseSubagents(data["subagents"])
	hasActiveSub := subActive > 0
	segSub := dim + "Sub 0" + reset
	if subActive > 0 {
		segSub = fmt.Sprintf("%sSub %d/%d%s", cSub, subActive, subTotal, reset)
	} else if subTotal > 0 {
		segSub = fmt.Sprintf("%sSub 0/%d%s", dim, subTotal, reset)
	}

	// 6. Tasks / Artifacts
	segTasks := dim + "Task 0/0" + reset
	if done, total, ok := resolveBrainTasks(brainDir); ok {
		segTasks = fmt.Sprintf("%sTask %d/%d%s", cTask, done, total, reset)
	} else if artifactCount > 0 {
		segTasks = fmt.Sprintf("%sArt %d%s", cTask, artifactCount, reset)
	}

	// Quotas
	quota := asMap(data["quota"])
	mLower := strings.ToLower(mID + " " + mName)
	wkKey, h5Key := "gemini-weekly", "gemini-5h"
	if strings.Contains(mLower, "claude") || strings.Contains(mLower, "gpt") {
		wkKey, h5Key = "3p-weekly", "3p-5h"
	} else if strings.Contains(mLower, "argon") {
		wkKey = "gemini-argon-wtu-weekly"
	}
	wkFrac, wkOk := asFloat(asMap(quota[wkKey])["remaining_fraction"])
	h5Frac, h5Ok := asFloat(asMap(quota[h5Key])["remaining_fraction"])
	wkPct := clampPct(wkFrac * 100)
	h5Pct := clampPct(h5Frac * 100)

	// Context Window (prefer live transcript.jsonl when higher/newer than stdin)
	ctxObj := asMap(data["context_window"])
	maxTokens := asInt(ctxObj["context_window_size"])
	if maxTokens <= 0 {
		maxTokens = 1048576
	}
	ctxPct, ctxOk := asFloat(ctxObj["used_percentage"])
	if !ctxOk {
		if inTok, okIn := asFloat(ctxObj["total_input_tokens"]); okIn && inTok > 0 {
			ctxPct = (inTok * 100) / float64(maxTokens)
		} else {
			ctxPct = 0
		}
	}
	if livePct, ok := resolveLiveCtxPct(brainDir, transcriptPath, maxTokens); ok && livePct > ctxPct {
		ctxPct = livePct
	}
	ctxPct = clampPct(ctxPct)

	segCtxBar := fmtMetric("Ctx", ctxPct, true, "bar", true)
	segCtxDec := fmtMetric("Ctx", ctxPct, true, "dec", true)
	segCtxInt := fmtMetric("Ctx", ctxPct, true, "int", true)

	// Emergency single metric (lowest quota <= 20%, otherwise Ctx)
	wkInt := int(math.Round(wkPct))
	h5Int := int(math.Round(h5Pct))
	segDanger := segCtxInt
	if h5Ok && h5Int <= 20 && (!wkOk || h5Int <= wkInt) {
		segDanger = fmtMetric("5h", h5Pct, true, "int", false)
	} else if wkOk && wkInt <= 20 {
		segDanger = fmtMetric("Wk", wkPct, true, "int", false)
	}

	// Responsive Selection & Alignment
	sep := " " + dim + "│" + reset + " "
	leftFull := strings.Join([]string{segSession, segModelFull, segMode, segStatus, segSub, segTasks}, sep)
	leftSplitSub := strings.Join([]string{segModelShort, segMode, segStatus, segSub}, sep)
	leftSplit := strings.Join([]string{segModelShort, segMode, segStatus}, sep)
	leftSidebar := strings.Join([]string{segModelMini, segStatus}, sep)

	rightFull := strings.Join([]string{
		fmtMetric("Wk", wkPct, wkOk, "bar", false),
		fmtMetric("5h", h5Pct, h5Ok, "bar", false),
		segCtxBar,
	}, sep)
	rightDec := strings.Join([]string{
		fmtMetric("Wk", wkPct, wkOk, "dec", false),
		fmtMetric("5h", h5Pct, h5Ok, "dec", false),
		segCtxDec,
	}, sep)
	rightInt := strings.Join([]string{
		fmtMetric("Wk", wkPct, wkOk, "int", false),
		fmtMetric("5h", h5Pct, h5Ok, "int", false),
		segCtxInt,
	}, sep)

	type candidate struct {
		left  string
		right string
	}
	candidates := []candidate{
		{leftFull, rightFull},
		{leftFull, rightDec},
	}
	if hasActiveSub {
		candidates = append(candidates, candidate{leftSplitSub, rightDec})
	}
	candidates = append(candidates,
		candidate{leftSplit, rightDec},
		candidate{leftSidebar, rightInt},
		candidate{leftSidebar, segDanger},
		candidate{segStatus, segDanger},
	)

	chosenLeft, chosenRight := segStatus, segDanger
	for _, cand := range candidates {
		if visibleLen(cand.left)+visibleLen(cand.right)+2 <= cols {
			chosenLeft, chosenRight = cand.left, cand.right
			break
		}
	}

	pad := cols - visibleLen(chosenLeft) - visibleLen(chosenRight)
	spacer := "  " + dim + "│" + reset + "  "
	if pad >= 2 {
		spacer = strings.Repeat(" ", pad)
	}
	return chosenLeft + spacer + chosenRight
}

func resolveCols(data map[string]any, ttyWidthFn func() int) int {
	for _, candidate := range []any{
		data["terminal_width"],
		asMap(data["terminal"])["columns"],
		asMap(data["dimensions"])["columns"],
		asMap(data["viewport"])["width"],
		data["columns"],
		data["width"],
	} {
		if c := asInt(candidate); c > 0 {
			return c
		}
	}
	if ttyWidthFn != nil {
		if c := ttyWidthFn(); c > 0 {
			return c
		}
	}
	if c, err := strconv.Atoi(strings.TrimSpace(os.Getenv("COLUMNS"))); err == nil && c > 0 {
		return c
	}
	return 140
}

func resolveBrainDir(homeDir, sid, transcriptPath string) string {
	var candidates []string
	if transcriptPath != "" {
		slashPath := filepath.ToSlash(transcriptPath)
		slashCLI := strings.Replace(slashPath, "/.gemini/antigravity/", "/.gemini/antigravity-cli/", 1)
		if idx := strings.Index(slashCLI, "/.system_generated/"); idx > 0 {
			candidates = append(candidates, filepath.FromSlash(slashCLI[:idx]))
		}
		if sid != "" && homeDir != "" {
			candidates = append(candidates, filepath.Join(homeDir, ".gemini", "antigravity-cli", "brain", sid))
		}
		if idx := strings.Index(slashPath, "/.system_generated/"); idx > 0 {
			candidates = append(candidates, filepath.FromSlash(slashPath[:idx]))
		}
	} else if sid != "" && homeDir != "" {
		candidates = append(candidates, filepath.Join(homeDir, ".gemini", "antigravity-cli", "brain", sid))
	}
	if sid != "" && homeDir != "" {
		candidates = append(candidates, filepath.Join(homeDir, ".gemini", "antigravity", "brain", sid))
	}

	for _, c := range candidates {
		if c != "" {
			if st, err := os.Stat(c); err == nil && st.IsDir() {
				return c
			}
		}
	}
	return ""
}

func resolveLiveCtxPct(brainDir, transcriptPath string, maxTokens int) (float64, bool) {
	if maxTokens <= 0 {
		return 0, false
	}
	var logFile string
	if brainDir != "" {
		candidate := filepath.Join(brainDir, ".system_generated", "logs", "transcript.jsonl")
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			logFile = candidate
		}
	}
	if logFile == "" && transcriptPath != "" {
		if st, err := os.Stat(transcriptPath); err == nil && !st.IsDir() {
			logFile = transcriptPath
		}
	}
	if logFile == "" {
		return 0, false
	}

	f, err := os.Open(logFile)
	if err != nil {
		return 0, false
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil || st.Size() == 0 {
		return 0, false
	}

	const maxTail = int64(524288)
	start := int64(0)
	if st.Size() > maxTail {
		start = st.Size() - maxTail
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return 0, false
	}
	buf, err := io.ReadAll(f)
	if err != nil {
		return 0, false
	}

	for len(buf) > 0 {
		idx := bytes.LastIndexByte(buf, '\n')
		var line []byte
		if idx >= 0 {
			line = bytes.TrimSpace(buf[idx+1:])
			buf = buf[:idx]
		} else {
			line = bytes.TrimSpace(buf)
			buf = nil
		}
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal(line, &obj); err != nil || obj == nil {
			continue
		}
		if obj["input_tokens"] == nil && obj["cache_read_tokens"] == nil {
			continue
		}
		inTok, _ := asFloat(obj["input_tokens"])
		cacheTok, _ := asFloat(obj["cache_read_tokens"])
		outTok, _ := asFloat(obj["output_tokens"])
		used := inTok + cacheTok + outTok
		if used > 0 {
			return (used * 100.0) / float64(maxTokens), true
		}
	}
	return 0, false
}

func resolveSessionTitle(homeDir, sid string) string {
	if homeDir == "" || sid == "" {
		return ""
	}
	metaPath := filepath.Join(homeDir, ".gemini", "antigravity-cli", "cache", "conversation_metadata.json")
	raw, err := os.ReadFile(metaPath)
	if err != nil {
		return ""
	}
	var root any
	if err := json.Unmarshal(raw, &root); err != nil {
		return ""
	}
	return findTitleInJSON(root, strings.ToLower(sid))
}

func findTitleInJSON(node any, targetLower string) string {
	switch v := node.(type) {
	case map[string]any:
		for k, child := range v {
			if strings.ToLower(k) == targetLower {
				if childMap, ok := child.(map[string]any); ok {
					if t := extractTitleField(childMap, targetLower); t != "" {
						return t
					}
				}
			}
		}
		hasMatchingVal := false
		for _, child := range v {
			if s, ok := child.(string); ok && strings.ToLower(strings.TrimSpace(s)) == targetLower {
				hasMatchingVal = true
				break
			}
		}
		if hasMatchingVal {
			if t := extractTitleField(v, targetLower); t != "" {
				return t
			}
		}
		for _, child := range v {
			if t := findTitleInJSON(child, targetLower); t != "" {
				return t
			}
		}
	case []any:
		for _, item := range v {
			if t := findTitleInJSON(item, targetLower); t != "" {
				return t
			}
		}
	}
	return ""
}

func extractTitleField(m map[string]any, targetLower string) string {
	for _, key := range []string{"Title", "title", "Name", "name"} {
		if s, ok := m[key].(string); ok {
			s = strings.TrimSpace(s)
			if s != "" && strings.ToLower(s) != targetLower {
				return s
			}
		}
	}
	return ""
}

func resolveBrainTasks(brainDir string) (int, int, bool) {
	if brainDir == "" {
		return 0, 0, false
	}
	f, err := os.Open(filepath.Join(brainDir, "task.md"))
	if err != nil {
		return 0, 0, false
	}
	defer f.Close()

	done, total := 0, 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		trimmed := strings.TrimLeft(scanner.Text(), " \t-*")
		if strings.HasPrefix(trimmed, "[x]") || strings.HasPrefix(trimmed, "[X]") {
			done++
			total++
		} else if strings.HasPrefix(trimmed, "[ ]") {
			total++
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, 0, false
	}
	return done, total, total > 0
}

func makeBar(pct int, color string) string {
	if pct < 0 {
		pct = 0
	} else if pct > 100 {
		pct = 100
	}
	filled := pct * 6 / 100
	if pct > 0 && filled == 0 {
		filled = 1
	}
	return color + "[" + strings.Repeat("█", filled) + dim + strings.Repeat("·", 6-filled) + reset + color + "]"
}

func fmtMetric(label string, pct float64, ok bool, mode string, invert bool) string {
	if !ok {
		if mode == "bar" {
			return dim + label + " [······] --" + reset
		}
		return dim + label + " --" + reset
	}
	pct = clampPct(pct)
	rounded := int(math.Round(pct))
	color := green
	if invert {
		if rounded >= 80 {
			color = red
		} else if rounded >= 60 {
			color = yellow
		}
	} else {
		if rounded <= 10 {
			color = red
		} else if rounded <= 30 {
			color = yellow
		}
	}
	switch mode {
	case "bar":
		return fmt.Sprintf("%s%s %s %.2f%%%s", color, label, makeBar(rounded, color), pct, reset)
	case "dec":
		return fmt.Sprintf("%s%s %.2f%%%s", color, label, pct, reset)
	default:
		return fmt.Sprintf("%s%s %d%%%s", color, label, rounded, reset)
	}
}

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

func isValidTitle(title, sid string) bool {
	if title == "" || title == "-" || title == sid {
		return false
	}
	if strings.HasPrefix(title, "/") || strings.HasPrefix(title, "~/") {
		return false
	}
	if len(title) >= 32 && isHexOrDash(title) {
		return false
	}
	return true
}

func isHexOrDash(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		isHex := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') || c == '-'
		if !isHex {
			return false
		}
	}
	return true
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

func clampPct(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func firstStr(vals ...any) string {
	for _, v := range vals {
		if s, ok := v.(string); ok {
			s = strings.TrimSpace(s)
			if s != "" && s != "-" {
				return s
			}
		}
	}
	return ""
}

func asFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, !math.IsNaN(n) && !math.IsInf(n, 0)
	case int:
		return float64(n), true
	case string:
		s := strings.TrimSpace(n)
		if s == "" || s == "-" {
			return 0, false
		}
		f, err := strconv.ParseFloat(s, 64)
		return f, err == nil && !math.IsNaN(f) && !math.IsInf(f, 0)
	}
	return 0, false
}

func asInt(v any) int {
	if f, ok := asFloat(v); ok {
		return int(math.Round(f))
	}
	return 0
}
