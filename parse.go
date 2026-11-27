package main

import (
	"math"
	"strconv"
	"strings"
)

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
