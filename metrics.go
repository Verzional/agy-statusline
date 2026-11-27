package main

import (
	"fmt"
	"math"
	"strings"
	"time"
)

type quotaMetrics struct {
	wkPct   float64
	wkOk    bool
	wkReset string
	h5Pct   float64
	h5Ok    bool
	h5Reset string
}

const barWidth = 7

func resolveQuotas(quota map[string]any, modelID, modelName string) quotaMetrics {
	mLower := strings.ToLower(modelID + " " + modelName)
	wkKey, h5Key := "gemini-weekly", "gemini-5h"
	if strings.Contains(mLower, "claude") || strings.Contains(mLower, "gpt") {
		wkKey, h5Key = "3p-weekly", "3p-5h"
	} else if strings.Contains(mLower, "argon") {
		wkKey = "gemini-argon-wtu-weekly"
	}

	wkObj := asMap(quota[wkKey])
	h5Obj := asMap(quota[h5Key])
	wkFrac, wkOk := asFloat(wkObj["remaining_fraction"])
	h5Frac, h5Ok := asFloat(h5Obj["remaining_fraction"])

	return quotaMetrics{
		wkPct:   clampPct(wkFrac * 100),
		wkOk:    wkOk,
		wkReset: formatQuotaReset(wkObj),
		h5Pct:   clampPct(h5Frac * 100),
		h5Ok:    h5Ok,
		h5Reset: formatQuotaReset(h5Obj),
	}
}

func resolveContextPct(ctxObj map[string]any, brainDir, transcriptPath string) float64 {
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
	return clampPct(ctxPct)
}

func bgForColor(color string) string {
	switch color {
	case red:
		return bgRed
	case yellow:
		return bgYellow
	default:
		return bgGreen
	}
}

func formatQuotaReset(obj map[string]any) string {
	var d time.Duration
	if rt := firstStr(obj["reset_time"], obj["resets_at"]); rt != "" {
		if t, err := time.Parse(time.RFC3339Nano, rt); err == nil {
			d = time.Until(t)
		} else if secs, ok := asFloat(obj["reset_in_seconds"]); ok {
			d = time.Duration(secs * float64(time.Second))
		}
	} else if secs, ok := asFloat(obj["reset_in_seconds"]); ok {
		d = time.Duration(secs * float64(time.Second))
	}
	if d <= 0 {
		return ""
	}
	if d < time.Minute {
		return "<1m"
	}

	totalMins := int(d.Minutes())
	days := totalMins / 1440
	hours := (totalMins % 1440) / 60
	mins := totalMins % 60

	switch {
	case days > 0:
		return fmt.Sprintf("%dd%02dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh%02dm", hours, mins)
	default:
		return fmt.Sprintf("%02dm", mins)
	}
}

func makeBar(pct int, color, overlay string) string {
	if pct < 0 {
		pct = 0
	} else if pct > 100 {
		pct = 100
	}
	filled := (pct*barWidth + 50) / 100
	if pct > 0 && filled == 0 {
		filled = 1
	} else if pct < 100 && filled == barWidth {
		filled = barWidth - 1
	}
	if overlay == "" {
		return color + "[" + strings.Repeat("█", filled) + dim + strings.Repeat("·", barWidth-filled) + reset + color + "]"
	}

	var cells [barWidth]rune
	for i := range cells {
		cells[i] = ' '
	}
	runes := []rune(overlay)
	if len(runes) > barWidth {
		runes = runes[:barWidth]
	}
	offset := (barWidth - len(runes)) / 2
	copy(cells[offset:], runes)

	bg := bgForColor(color)

	var b strings.Builder
	b.WriteString(color)
	b.WriteByte('[')
	inFill := false
	for i, ch := range cells {
		wantFill := i < filled
		if i == 0 || wantFill != inFill {
			b.WriteString(reset)
			if wantFill {
				b.WriteString(bg)
				b.WriteString(fgInk)
				b.WriteString(bold)
			} else {
				b.WriteString(bgTrack)
				b.WriteString(color)
			}
			inFill = wantFill
		}
		b.WriteRune(ch)
	}
	b.WriteString(reset)
	b.WriteString(color)
	b.WriteByte(']')
	return b.String()
}

func fmtMetric(label string, pct float64, ok bool, mode string, invert bool, overlay ...string) string {
	ov := ""
	if len(overlay) > 0 {
		ov = overlay[0]
	}
	if !ok {
		if mode == "bar" {
			return dim + label + " [" + strings.Repeat("·", barWidth) + "] --" + reset
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
		return fmt.Sprintf("%s%s %s %.2f%%%s", color, label, makeBar(rounded, color, ov), pct, reset)
	case "dec":
		return fmt.Sprintf("%s%s %.2f%%%s", color, label, pct, reset)
	default:
		return fmt.Sprintf("%s%s %d%%%s", color, label, rounded, reset)
	}
}
