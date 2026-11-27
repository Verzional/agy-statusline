package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
)

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
