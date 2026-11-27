package main

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFormatQuotaReset(t *testing.T) {
	tests := []struct {
		name string
		obj  map[string]any
		want string
	}{
		{"empty", map[string]any{}, ""},
		{"expired", map[string]any{"reset_time": time.Now().Add(-5 * time.Minute).UTC().Format(time.RFC3339)}, ""},
		{"rfc3339_future", map[string]any{"reset_time": time.Now().Add(95*time.Minute + 30*time.Second).UTC().Format(time.RFC3339)}, "1h35m"},
		{"under_1m", map[string]any{"reset_in_seconds": float64(30)}, "<1m"},
		{"minutes", map[string]any{"reset_in_seconds": float64(42 * 60)}, "42m"},
		{"hours_exact", map[string]any{"reset_in_seconds": float64(3 * 3600)}, "3h"},
		{"hours_mins", map[string]any{"reset_in_seconds": float64(3*3600 + 12*60)}, "3h12m"},
		{"days_hours", map[string]any{"reset_in_seconds": float64(4*86400 + 12*3600)}, "4d12h"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatQuotaReset(tc.obj); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMakeBarVisibleWidth(t *testing.T) {
	cases := []struct {
		pct     int
		color   string
		overlay string
	}{
		{0, red, ""},
		{0, red, "42m"},
		{52, green, "3h12m"},
		{93, green, "4d12h"},
		{100, yellow, "23h59m"},
		{100, green, "too-long-text"},
	}

	for _, tc := range cases {
		bar := makeBar(tc.pct, tc.color, tc.overlay)
		if got := visibleLen(bar); got != 9 {
			t.Errorf("makeBar(%d, %q) visibleLen = %d, want 9 (raw=%q)", tc.pct, tc.overlay, got, bar)
		}
	}
}

func TestResolveQuotas(t *testing.T) {
	quota := map[string]any{
		"gemini-weekly":           map[string]any{"remaining_fraction": 0.90, "reset_in_seconds": float64(3 * 86400)},
		"gemini-5h":               map[string]any{"remaining_fraction": 0.75, "reset_in_seconds": float64(2 * 3600)},
		"gemini-argon-wtu-weekly": map[string]any{"remaining_fraction": 0.60, "reset_in_seconds": float64(5 * 86400)},
		"3p-weekly":               map[string]any{"remaining_fraction": 0.45, "reset_in_seconds": float64(4 * 86400)},
		"3p-5h":                   map[string]any{"remaining_fraction": 0.20, "reset_in_seconds": float64(45 * 60)},
	}

	tests := []struct {
		name      string
		quota     map[string]any
		modelID   string
		modelName string
		wantWk    float64
		wantWkOk  bool
		wantWkRst string
		wantH5    float64
		wantH5Ok  bool
		wantH5Rst string
	}{
		{"gemini_default", quota, "gemini-4-neon", "Gemini 4 Neon", 90, true, "3d", 75, true, "2h"},
		{"argon_routing", quota, "gemini-4-argon", "Gemini 4 Argon", 60, true, "5d", 75, true, "2h"},
		{"claude_routing", quota, "claude-sonnet", "Claude Sonnet 5.5", 45, true, "4d", 20, true, "45m"},
		{"gpt_routing", quota, "", "GPT-OSS 120B (Medium)", 45, true, "4d", 20, true, "45m"},
		{"missing_buckets", map[string]any{}, "", "Gemini 4 Neon", 0, false, "", 0, false, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveQuotas(tc.quota, tc.modelID, tc.modelName)
			if got.wkOk != tc.wantWkOk || got.h5Ok != tc.wantH5Ok {
				t.Fatalf("ok mismatch: got (wk=%v, h5=%v), want (wk=%v, h5=%v)", got.wkOk, got.h5Ok, tc.wantWkOk, tc.wantH5Ok)
			}
			if math.Abs(got.wkPct-tc.wantWk) > 1e-6 || math.Abs(got.h5Pct-tc.wantH5) > 1e-6 {
				t.Fatalf("pct mismatch: got (wk=%.2f, h5=%.2f), want (wk=%.2f, h5=%.2f)", got.wkPct, got.h5Pct, tc.wantWk, tc.wantH5)
			}
			if got.wkReset != tc.wantWkRst || got.h5Reset != tc.wantH5Rst {
				t.Fatalf("reset mismatch: got (wk=%q, h5=%q), want (wk=%q, h5=%q)", got.wkReset, got.h5Reset, tc.wantWkRst, tc.wantH5Rst)
			}
		})
	}
}

func TestResolveBrainTasks(t *testing.T) {
	t.Run("counts_checkboxes", func(t *testing.T) {
		dir := t.TempDir()
		content := strings.Join([]string{
			"# Plan",
			"- [x] First task",
			"  * [X] Uppercase done",
			"- [ ] Pending task",
			"	- [ ] Indented pending",
			"Regular note line",
		}, "\n")
		if err := os.WriteFile(filepath.Join(dir, "task.md"), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		done, total, ok := resolveBrainTasks(dir)
		if !ok || done != 2 || total != 4 {
			t.Fatalf("got (%d, %d, %v), want (2, 4, true)", done, total, ok)
		}
	})

	t.Run("no_checkboxes", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "task.md"), []byte("# Notes\nNothing actionable yet.\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if done, total, ok := resolveBrainTasks(dir); ok || done != 0 || total != 0 {
			t.Fatalf("got (%d, %d, %v), want (0, 0, false)", done, total, ok)
		}
	})

	t.Run("missing_dir", func(t *testing.T) {
		if _, _, ok := resolveBrainTasks(""); ok {
			t.Fatal("expected ok=false for empty brainDir")
		}
	})
}

func TestResolveLiveCtxPct(t *testing.T) {
	t.Run("reads_latest_usage_from_brain_dir", func(t *testing.T) {
		brainDir := t.TempDir()
		logsDir := filepath.Join(brainDir, ".system_generated", "logs")
		if err := os.MkdirAll(logsDir, 0o755); err != nil {
			t.Fatal(err)
		}
		lines := strings.Join([]string{
			`{"input_tokens": 10000, "output_tokens": 500}`,
			`{"input_tokens": 20000, "cache_read_tokens": 30000, "output_tokens": 10000}`,
			`{"event": "tool_call", "name": "read_file"}`,
			`not-json-tail-fragment`,
		}, "\n")
		if err := os.WriteFile(filepath.Join(logsDir, "transcript.jsonl"), []byte(lines), 0o600); err != nil {
			t.Fatal(err)
		}

		got, ok := resolveLiveCtxPct(brainDir, "", 100000)
		if !ok || math.Abs(got-60.0) > 1e-6 {
			t.Fatalf("got (%.4f, %v), want (60.0, true)", got, ok)
		}
	})

	t.Run("falls_back_to_transcript_path", func(t *testing.T) {
		transcript := filepath.Join(t.TempDir(), "transcript.jsonl")
		if err := os.WriteFile(transcript, []byte(`{"cache_read_tokens": 25000, "output_tokens": 5000}`+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}

		got, ok := resolveLiveCtxPct("", transcript, 200000)
		if !ok || math.Abs(got-15.0) > 1e-6 {
			t.Fatalf("got (%.4f, %v), want (15.0, true)", got, ok)
		}
	})

	t.Run("invalid_max_tokens", func(t *testing.T) {
		if _, ok := resolveLiveCtxPct(t.TempDir(), "", 0); ok {
			t.Fatal("expected ok=false when maxTokens <= 0")
		}
	})
}

func TestResolveSessionTitle(t *testing.T) {
	writeMeta := func(t *testing.T, body string) string {
		t.Helper()
		home := t.TempDir()
		cacheDir := filepath.Join(home, ".gemini", "antigravity-cli", "cache")
		if err := os.MkdirAll(cacheDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(cacheDir, "conversation_metadata.json"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return home
	}

	t.Run("map_keyed_by_session_id", func(t *testing.T) {
		home := writeMeta(t, `{"abc-123": {"Title": "Bay-Rent"}}`)
		if got := resolveSessionTitle(home, "ABC-123"); got != "Bay-Rent" {
			t.Fatalf("got %q, want %q", got, "Bay-Rent")
		}
	})

	t.Run("nested_array_entry", func(t *testing.T) {
		home := writeMeta(t, `{"conversations": [{"id": "def-456", "title": "Statusline Polish"}]}`)
		if got := resolveSessionTitle(home, "DEF-456"); got != "Statusline Polish" {
			t.Fatalf("got %q, want %q", got, "Statusline Polish")
		}
	})

	t.Run("missing_session_id", func(t *testing.T) {
		home := writeMeta(t, `{"abc-123": {"Title": "Bay-Rent"}}`)
		if got := resolveSessionTitle(home, "zzz-999"); got != "" {
			t.Fatalf("got %q, want empty string", got)
		}
	})
}

func TestRenderStatuslineResponsiveTiers(t *testing.T) {
	payload := []byte(`{
		"conversation_title": "Bay-Rent",
		"model": "Gemini 4 Argon",
		"effort": "high",
		"cycle_mode": "accept-edits",
		"agent_state": "idle",
		"quota": {
			"gemini-argon-wtu-weekly": {"remaining_fraction": 0.93, "reset_in_seconds": 388800},
			"gemini-5h": {"remaining_fraction": 0.15, "reset_in_seconds": 4320}
		},
		"context_window": {"used_percentage": 13}
	}`)

	for _, cols := range []int{150, 100, 65, 40} {
		out := renderStatusline(payload, t.TempDir(), func() int { return cols })
		if got := visibleLen(out); got > cols {
			t.Errorf("cols=%d: rendered width %d overflows terminal", cols, got)
		}
	}

	narrow := renderStatusline(payload, t.TempDir(), func() int { return 40 })
	if !strings.Contains(narrow, "5h 15%") {
		t.Errorf("expected danger metric '5h 15%%' in narrow tier, got %q", narrow)
	}
}

func BenchmarkRenderStatusline(b *testing.B) {
	payload := []byte(`{
		"terminal_width": 140,
		"conversation_title": "Bay-Rent",
		"model": "Orchestrator (Neon)",
		"cycle_mode": "accept-edits",
		"agent_state": "idle",
		"quota": {
			"gemini-weekly": {"remaining_fraction": 0.9307, "reset_in_seconds": 388800},
			"gemini-5h": {"remaining_fraction": 0.9811, "reset_in_seconds": 17042}
		},
		"context_window": {"used_percentage": 13}
	}`)
	dir := b.TempDir()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = renderStatusline(payload, dir, func() int { return 140 })
	}
}