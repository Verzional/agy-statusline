# agy-statusline

High-performance, responsive two-cluster statusline for **Antigravity CLI (`agy`)** built in pure Bash for **macOS** and **Linux**.

## Responsive Layout

The statusline measures the exact rendered width of each segment and automatically steps down across layout tiers so it stays flush-right without wrapping—even in 50/50 split panes or narrow sidebars:

- **Wide (`135+` cols):**
  ```text
  Session │ Model (Effort) │ Mode │ Status │ Sub 0/24 │ Task 3/5          Wk [██████] 95.98% │ 5h [█████·] 86.41% │ Ctx [██····] 26%
  ```
- **Medium (`105–134` cols):** Drops ASCII bars while keeping color-coded decimals (`Wk 95.98% │ 5h 86.41% │ Ctx 26%`).
- **Half-Screen Split (`75–104` cols):** Strips vendor prefixes (`Hydrogen (Low) │ Edits │ Working`), dynamically surfaces `Sub active/total` when subagents are active, and preserves all three right-side percentages.
- **Sidebar (`50–74` cols):** Abbreviates effort (`Hydrogen (L) │ Working`) and rounds quotas to integers (`Wk 96% │ 5h 86% │ Ctx 26%`).
- **Emergency (`< 50` cols):** Keeps `Model (E) │ Status` on the left and displays whichever metric is most urgent on the right (lowest quota at `<= 20%`, otherwise `Ctx %`).

## Compatibility & Requirements

- **Shells:** Works seamlessly whether your interactive shell is **Zsh** or **Bash**—the script executes via `#!/usr/bin/env bash`, and all setup commands below are copy-paste compatible with both shells.
- **Operating Systems:** **macOS**, **Linux**, **WSL**, and **Windows** (via Git Bash).
- **Dependencies:**
  - `bash` (3.2+)
  - `jq` (`brew install jq` on macOS, `sudo apt install jq` / `sudo pacman -S jq` on Linux)

## Installation

All commands below work identically in **Zsh** and **Bash**.

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

Safely merge the `statusLine` block into your existing `settings.json` without overwriting other configuration keys:

```bash
SETTINGS="$HOME/.gemini/antigravity-cli/settings.json"
[ -f "$SETTINGS" ] \vert{}\vert{} printf '{}\n' > "$SETTINGS"

jq '.statusLine = {
  "type": "command",
  "command": "~/.gemini/antigravity-cli/statusline.sh",
  "enabled": true
}' "$SETTINGS" > "${SETTINGS}.tmp" && mv "${SETTINGS}.tmp" "$SETTINGS"
```

*(Optional)* Add `"stack_with_default": true` inside the `"statusLine"` object to render Antigravity's built-in status bar alongside this one.

### Step 3: Verify & launch

Test across full-screen (`153` cols) and half-screen split (`80` cols) widths:

```bash
for w in 153 80; do
  printf '{"terminal_width":%d,"conversation_title":"Responsive Statusline","model":{"display_name":"Gemini 4 Hydrogen (Low)"},"cycle_mode":"accept-edits","agent_state":"working","quota":{"gemini-weekly":{"remaining_fraction":0.9598},"gemini-5h":{"remaining_fraction":0.8641}},"context_window":{"used_percentage":26}}\n' "$w" \
    | ~/.gemini/antigravity-cli/statusline.sh
done
```

Start a new `agy` session (or run `/statusline` inside an active session) to enable it.

## Refresh Behavior

- **Context (`Ctx`):** Syncs live during turns directly from the active session's `transcript.jsonl`.
- **Quotas (`Wk` / `5h`):** Managed server-side by `agy` and refreshed on session startup, prompt submission, or `/usage`.

## Updating

If you installed via **Option A**, pull the latest changes—the symlink updates Antigravity CLI immediately:

```bash
git -C ~/agy-statusline pull
```
