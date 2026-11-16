# agy-statusline

Two-cluster statusline for **Antigravity CLI (`agy`)** featuring live PTY right-edge alignment, active/total subagent counters, brain `task.md` & artifact tracking, live `transcript.jsonl` context window sync, and model-aware two-decimal quota gauges.

## Layout

```text
Session │ Model (Effort) │ Mode │ Status │ Sub 0/24 │ Task 3/5          Wk [██████] 95.98% │ 5h [█████·] 86.41% │ Ctx [██····] 26%
```

- **Left Cluster (Execution):** Session title, active model & effort tier, cycle mode (`Plan`, `Edits`, `Auto`, `Ask`), agent state (`Idle`, `Working`, `Tool`, `Review`), active/total subagents, and `task.md` progress (falls back to `Art <count>` when no `task.md` exists).
- **Right Cluster (Usage):** Model-routed weekly (`Wk`) and 5-hour (`5h`) quota bars (`gemini-*`, `gemini-argon-wtu-weekly`, or `3p-*`) and live context window usage (`Ctx`).

## Requirements

- **macOS** (uses `stty -f` for PTY width fallback)
- **`jq`** (`brew install jq`)

## Installation

### Step 1: Install the script

#### Option A: Clone & symlink (Recommended — easy updates via `git pull`)

```bash
git clone [https://github.com/Verzional/agy-statusline.git](https://github.com/Verzional/agy-statusline.git) ~/agy-statusline
chmod +x ~/agy-statusline/statusline.sh
mkdir -p ~/.gemini/antigravity-cli
ln -sf ~/agy-statusline/statusline.sh ~/.gemini/antigravity-cli/statusline.sh
```

#### Option B: Standalone download (No repo clone)

```bash
mkdir -p ~/.gemini/antigravity-cli
curl -fsSL [https://raw.githubusercontent.com/Verzional/agy-statusline/main/statusline.sh](https://raw.githubusercontent.com/Verzional/agy-statusline/main/statusline.sh) \
  -o ~/.gemini/antigravity-cli/statusline.sh
chmod +x ~/.gemini/antigravity-cli/statusline.sh
```

### Step 2: Wire into `~/.gemini/antigravity-cli/settings.json`

Run this command to safely append the `statusLine` block to your existing `settings.json` without overwriting other settings:

```bash
SETTINGS="$HOME/.gemini/antigravity-cli/settings.json"
[[ -f "$SETTINGS" ]] \vert{}\vert{} echo '{}' > "$SETTINGS"

jq '.statusLine = {
  "type": "command",
  "command": "~/.gemini/antigravity-cli/statusline.sh",
  "enabled": true
}' "$SETTINGS" > "${SETTINGS}.tmp" && mv "${SETTINGS}.tmp" "$SETTINGS"
```

*(Optional)* Add `"stack_with_default": true` inside the `"statusLine"` object if you want Antigravity's built-in status bar rendered alongside this one.

### Step 3: Verify & launch

Test that the script executes cleanly with sample input:

```bash
echo '{"model":{"display_name":"Gemini 4 Hydrogen (Low)"},"context_window":{"used_percentage":25}}' \
  | ~/.gemini/antigravity-cli/statusline.sh
```

Then start a new `agy` session (or run `/statusline` inside an active session) to display it.

## Updating

If you used **Option A**, pull the latest changes—the symlink updates Antigravity CLI immediately:

```bash
git -C ~/agy-statusline pull
```
