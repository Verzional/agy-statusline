package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
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

	// 3. Mode & Status
	modeRaw := firstStr(data["cycle_mode"], data["permission_mode"], data["mode"])
	segMode := formatMode(modeRaw)

	aObj := asMap(data["agent_state"])
	statRaw := firstStr(aObj["state"], aObj["status"], data["agent_state"], data["status"], data["state"])
	segStatus := formatStatus(statRaw)

	// 4. Subagents
	subActive, subTotal := parseSubagents(data["subagents"])
	hasActiveSub := subActive > 0
	segSub := dim + "Sub 0" + reset
	if subActive > 0 {
		segSub = fmt.Sprintf("%sSub %d/%d%s", cSub, subActive, subTotal, reset)
	} else if subTotal > 0 {
		segSub = fmt.Sprintf("%sSub 0/%d%s", dim, subTotal, reset)
	}

	// 5. Tasks / Artifacts
	segTasks := dim + "Task 0/0" + reset
	if done, total, ok := resolveBrainTasks(brainDir); ok {
		segTasks = fmt.Sprintf("%sTask %d/%d%s", cTask, done, total, reset)
	} else if artifactCount > 0 {
		segTasks = fmt.Sprintf("%sArt %d%s", cTask, artifactCount, reset)
	}

	// 6. Quotas & Context Window
	q := resolveQuotas(asMap(data["quota"]), mID, mName)
	ctxPct := resolveContextPct(asMap(data["context_window"]), brainDir, transcriptPath)

	segCtxBar := fmtMetric("Ctx", ctxPct, true, "bar", true)
	segCtxDec := fmtMetric("Ctx", ctxPct, true, "dec", true)
	segCtxInt := fmtMetric("Ctx", ctxPct, true, "int", true)

	// Emergency single metric (lowest quota <= 20%, otherwise Ctx)
	wkInt := int(math.Round(q.wkPct))
	h5Int := int(math.Round(q.h5Pct))
	segDanger := segCtxInt
	if q.h5Ok && h5Int <= 20 && (!q.wkOk || h5Int <= wkInt) {
		segDanger = fmtMetric("5h", q.h5Pct, true, "int", false)
	} else if q.wkOk && wkInt <= 20 {
		segDanger = fmtMetric("Wk", q.wkPct, true, "int", false)
	}

	// Responsive Selection & Alignment
	sep := " " + dim + "│" + reset + " "
	leftFull := strings.Join([]string{segSession, segModelFull, segMode, segStatus, segSub, segTasks}, sep)
	leftSplitSub := strings.Join([]string{segModelShort, segMode, segStatus, segSub}, sep)
	leftSplit := strings.Join([]string{segModelShort, segMode, segStatus}, sep)
	leftSidebar := strings.Join([]string{segModelMini, segStatus}, sep)

	rightFull := strings.Join([]string{
		fmtMetric("Wk", q.wkPct, q.wkOk, "bar", false, q.wkReset),
		fmtMetric("5h", q.h5Pct, q.h5Ok, "bar", false, q.h5Reset),
		segCtxBar,
	}, sep)
	rightDec := strings.Join([]string{
		fmtMetric("Wk", q.wkPct, q.wkOk, "dec", false),
		fmtMetric("5h", q.h5Pct, q.h5Ok, "dec", false),
		segCtxDec,
	}, sep)
	rightInt := strings.Join([]string{
		fmtMetric("Wk", q.wkPct, q.wkOk, "int", false),
		fmtMetric("5h", q.h5Pct, q.h5Ok, "int", false),
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
