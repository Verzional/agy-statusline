# agy-statusline

Two-cluster statusline for Antigravity CLI featuring live PTY right-edge alignment, session title lookup, and model-aware two-decimal quota gauges.

## Requirements

- `jq`
- macOS (`stty -f`)

## Installation

### 1. Place and make executable

```bash
mkdir -p ~/.gemini/antigravity-cli
cp statusline.sh ~/.gemini/antigravity-cli/statusline.sh
chmod +x ~/.gemini/antigravity-cli/statusline.sh
```

### 2. Configure `settings.json`

Add the `statusLine` block to `~/.gemini/antigravity-cli/settings.json`:

```json
{
  "statusLine": {
    "type": "command",
    "command": "~/.gemini/antigravity-cli/statusline.sh"
  }
}
```

### 3. Enable in Antigravity CLI

Start a new `agy` session or run `/statusline` inside an active session to toggle it on.
