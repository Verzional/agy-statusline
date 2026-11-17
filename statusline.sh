#!/usr/bin/env bash
set -euo pipefail
shopt -s extglob nocasematch

case "${OSTYPE:-}" in
  darwin*) export LC_ALL=en_US.UTF-8 2>/dev/null || true ;;
  *)       export LC_ALL=C.UTF-8 2>/dev/null || export LC_ALL=en_US.UTF-8 2>/dev/null || true ;;
esac

IFS= read -rd '' input < /dev/stdin || true

# --- ANSI Colors (Raw ESC via $'...' for zero-%b formatting) ---
C_SESSION=$'\033[38;5;141m'   # Lavender
C_MODEL=$'\033[38;5;210m'     # Coral
C_MODE_PLAN=$'\033[38;5;117m' # Sky
C_MODE_EDIT=$'\033[38;5;221m' # Warm Gold
C_MODE_AUTO=$'\033[38;5;212m' # Pink
C_MODE_ASK=$'\033[38;5;156m'  # Lime
C_STAT_IDLE=$'\033[38;5;114m' # Mint
C_STAT_WORK=$'\033[38;5;208m' # Tangerine
C_STAT_TOOL=$'\033[38;5;80m'  # Turquoise
C_STAT_REV=$'\033[38;5;176m'  # Orchid
C_SUB=$'\033[38;5;81m'        # Electric Cyan
C_TASK=$'\033[38;5;229m'      # Warm Cream

GREEN=$'\033[32m'
YELLOW=$'\033[33m'
RED=$'\033[31m'
DIM=$'\033[2m'
RESET=$'\033[0m'

# --- Single-Pass JSON Extraction & Quota Pre-Computation ---
IFS=$'\t' read -r \
  term_cols raw_sid payload_title transcript_path artifact_count \
  model_name effort \
  wk_dec wk_int h5_dec h5_int \
  ctx_pct ctx_size cycle_mode live_status sub_active sub_total < <(
  printf '%s' "$input" | jq -r '
    def fmt_dec:
      if . == null or . == "-" then "-"
      else
        ((. * 10000 | round) / 100 | tostring) as $s |
        if ($s | test("\\.")) then
          if ($s | test("\\.[0-9]$")) then $s + "0" else $s end
        else $s + ".00" end
      end;
    def fmt_int:
      if . == null or . == "-" then "-"
      else (. * 100 | round | tostring) end;

    (if (.model | type) == "object" then (.model.id // "-") else "-" end) as $mid |
    (if (.model | type) == "object" then (.model.display_name // .model.name // .model.id // "default") else (.model // "default") end) as $mname |
    (($mid + " " + $mname) | ascii_downcase) as $mlc |
    (if ($mlc | contains("claude") or contains("gpt")) then .quota["3p-weekly"].remaining_fraction
     elif ($mlc | contains("argon")) then .quota["gemini-argon-wtu-weekly"].remaining_fraction
     else .quota["gemini-weekly"].remaining_fraction end) as $wk |
    (if ($mlc | contains("claude") or contains("gpt")) then .quota["3p-5h"].remaining_fraction
     else .quota["gemini-5h"].remaining_fraction end) as $h5 |

    [
      (.terminal_width // .terminal.columns // .dimensions.columns // .viewport.width // .columns // .width // 0),
      (.conversation_id // .session_id // "-"),
      (.conversation_title // .session_title // .title // "-"),
      (.transcript_path // "-"),
      (.artifact_count // 0),
      $mname,
      (if (.model | type) == "object" then (.model.effort // "-") else "-" end),
      ($wk | fmt_dec),
      ($wk | fmt_int),
      ($h5 | fmt_dec),
      ($h5 | fmt_int),
      (
        .context_window.used_percentage //
        (if ((.context_window.total_input_tokens // 0) > 0 and (.context_window.context_window_size // 0) > 0)
         then ((.context_window.total_input_tokens * 100) / .context_window.context_window_size | floor)
         else 0 end)
      ),
      (.context_window.context_window_size // 1048576),
      (.cycle_mode // .permission_mode // .mode // "-"),
      (if (.agent_state | type) == "object" then (.agent_state.state // .agent_state.status // "-") else (.agent_state // .status // .state // "-") end),
      (
        if (.subagents | type) == "array" then
          ([.subagents[] | select(.status == "running" or .status == "working" or .status == "in_progress" or .status == "active")] | length)
        elif (.subagents | type) == "object" then (.subagents.active // .subagents.running // 0)
        elif (.subagents | type) == "number" then .subagents
        else 0 end
      ),
      (
        if (.subagents | type) == "array" then (.subagents | length)
        elif (.subagents | type) == "object" then (.subagents.total // .subagents.count // 0)
        elif (.subagents | type) == "number" then .subagents
        else 0 end
      )
    ] | map(tostring | gsub("[\t\r\n]+"; " ")) | @tsv
  '
)

# --- Resolve Brain Directory ---
resolve_brain_dir() {
  local sid="$1" tpath="$2" candidate
  local tpath_cli="${tpath/\/.gemini\/antigravity\//\/.gemini\/antigravity-cli\/}"

  for candidate in \
    "${tpath_cli%%/.system_generated/*}" \
    "$HOME/.gemini/antigravity-cli/brain/${sid}" \
    "${tpath%%/.system_generated/*}" \
    "$HOME/.gemini/antigravity/brain/${sid}"; do
    if [[ -n "$candidate" && "$candidate" != "-" && -d "$candidate" ]]; then
      printf '%s' "$candidate"
      return
    fi
  done
}

brain_dir=$(resolve_brain_dir "$raw_sid" "$transcript_path")

# --- Live Context Usage (Cached by transcript mtime:size) ---
resolve_live_ctx_pct() {
  local bdir="$1" tpath="$2" max_tokens="$3" sid="$4"
  local log_file=""

  if [[ -n "$bdir" && -f "${bdir}/.system_generated/logs/transcript.jsonl" ]]; then
    log_file="${bdir}/.system_generated/logs/transcript.jsonl"
  elif [[ "$tpath" != "-" && -f "$tpath" ]]; then
    log_file="$tpath"
  fi

  [[ -z "$log_file" || ! "$max_tokens" =~ ^[0-9]+$ || "$max_tokens" -le 0 ]] && return

  local stat_sig=""
  case "${OSTYPE:-}" in
    darwin*) stat_sig=$(stat -f '%m:%z' "$log_file" 2>/dev/null || true) ;;
    *)       stat_sig=$(stat -c '%Y:%s' "$log_file" 2>/dev/null || true) ;;
  esac

  local cache="${TMPDIR:-/tmp}/agy-ctx-${sid}"
  if [[ -n "$stat_sig" && -f "$cache" ]]; then
    local cached_sig cached_pct
    IFS=' ' read -r cached_sig cached_pct < "$cache" || true
    if [[ "$cached_sig" == "$stat_sig" && "$cached_pct" =~ ^[0-9]+$ ]]; then
      printf '%s' "$cached_pct"
      return
    fi
  fi

  local used_tokens
  used_tokens=$(tail -n 120 "$log_file" 2>/dev/null | jq -R '
    fromjson? | select(type == "object" and (.input_tokens != null or .cache_read_tokens != null)) |
    ((.input_tokens // 0) + (.cache_read_tokens // 0) + (.output_tokens // 0))
  ' 2>/dev/null | tail -n 1 || true)

  if [[ "$used_tokens" =~ ^[0-9]+$ ]] && (( used_tokens > 0 )); then
    local computed=$(( used_tokens * 100 / max_tokens ))
    [[ -n "$stat_sig" ]] && printf '%s %s' "$stat_sig" "$computed" > "$cache"
    printf '%s' "$computed"
  fi
}

# --- Fallback Session Title Lookup ---
resolve_session_title() {
  local sid="$1"
  local meta="$HOME/.gemini/antigravity-cli/cache/conversation_metadata.json"
  local cache="${TMPDIR:-/tmp}/agy-title-${sid}"

  if [[ -f "$cache" && -f "$meta" && ! "$meta" -nt "$cache" ]]; then
    IFS= read -rd '' cached_title < "$cache" || true
    printf '%s' "$cached_title"
    return
  fi

  local found=""
  if [[ -f "$meta" ]]; then
    found=$(jq -r --arg sid "$sid" '
      ($sid | ascii_downcase) as $target |
      [
        .. | objects |
        (
          (to_entries[] | select((.key | ascii_downcase) == $target) | .value | objects | (.Title // .title // .Name // .name // empty)),
          (select(any(.[]; type == "string" and (ascii_downcase == $target))) | (.Title // .title // .Name // .name // empty))
        )
      ]
      | map(select(type == "string" and length > 0 and (ascii_downcase != $target)))
      | first // empty
    ' "$meta" 2>/dev/null || true)
  fi

  printf '%s' "$found" > "$cache"
  printf '%s' "$found"
}

# --- Pure-Bash Brain Task Counter (task.md checkboxes) ---
resolve_brain_tasks() {
  local task_file="${1}/task.md"
  [[ -z "$1" || ! -f "$task_file" ]] && return

  local done=0 total=0 line
  while IFS= read -r line || [[ -n "$line" ]]; do
    case "$line" in
      *([-*[:space:]])\[[xX]\]*) done=$((done + 1)); total=$((total + 1)) ;;
      *([-*[:space:]])\[\ \]*)   total=$((total + 1)) ;;
    esac
  done < "$task_file"

  (( total > 0 )) && printf '%d/%d' "$done" "$total"
}

# --- Pure-Bash UTF-8 Bar Builder ---
make_bar() {
  local pct_int="$1" color="$2" width=6
  (( pct_int < 0 )) && pct_int=0
  (( pct_int > 100 )) && pct_int=100
  local filled=$(( pct_int * width / 100 ))
  (( pct_int > 0 && filled == 0 )) && filled=1
  local empty=$(( width - filled ))
  local f_str="" e_str="" i
  for (( i=0; i<filled; i++ )); do f_str+="█"; done
  for (( i=0; i<empty; i++ )); do e_str+="·"; done
  printf '%s[%s%s%s%s]' "$color" "$f_str" "$DIM" "$e_str" "$RESET$color"
}

# ================= LEFT SEGMENTS =================

# 1. Session Name (capped at 18 chars in pure Bash)
session_label=""
if [[ "$payload_title" != "-" && -n "$payload_title" && "$payload_title" != "$raw_sid" ]]; then
  session_label="$payload_title"
elif [[ "$raw_sid" != "-" && -n "$raw_sid" ]]; then
  session_label=$(resolve_session_title "$raw_sid")
fi

if [[ -n "$session_label" ]]; then
  if (( ${#session_label} > 18 )); then
    session_label="${session_label:0:17}…"
  fi
  seg_session="${C_SESSION}${session_label}${RESET}"
elif [[ "$raw_sid" != "-" && -n "$raw_sid" ]]; then
  seg_session="${DIM}${raw_sid:0:8}${RESET}"
else
  seg_session="${DIM}--${RESET}"
fi

# 2. Model Variants (Pure Bash)
if [[ "$effort" != "-" && -n "$effort" && "$model_name" != *"(${effort})"* ]]; then
  case "$effort" in
    low)    effort_cap="Low" ;;
    medium) effort_cap="Medium" ;;
    high)   effort_cap="High" ;;
    none)   effort_cap="None" ;;
    *)      effort_cap="$effort" ;;
  esac
  model_name="${model_name} (${effort_cap})"
fi

short_model="$model_name"
case "$short_model" in
  Gemini\ [0-9]*\ *) short_model="${short_model#Gemini [0-9]* }" ;;
  Gemini\ *)         short_model="${short_model#Gemini }" ;;
  Claude\ *)         short_model="${short_model#Claude }" ;;
  OpenAI\ *)         short_model="${short_model#OpenAI }" ;;
esac
[[ -z "$short_model" ]] && short_model="$model_name"

mini_model="$short_model"
if [[ "$mini_model" =~ ^(.*)\(([A-Za-z])[A-Za-z]*\)$ ]]; then
  mini_model="${BASH_REMATCH[1]}(${BASH_REMATCH[2]})"
fi

seg_model_full="${C_MODEL}${model_name}${RESET}"
seg_model_short="${C_MODEL}${short_model}${RESET}"
seg_model_mini="${C_MODEL}${mini_model}${RESET}"

# 3. Mode (nocasematch active)
case "$cycle_mode" in
  plan)                          seg_mode="${C_MODE_PLAN}Plan${RESET}" ;;
  accept-edits|acceptedits|edit) seg_mode="${C_MODE_EDIT}Edits${RESET}" ;;
  auto|yolo|bypass*)             seg_mode="${C_MODE_AUTO}Auto${RESET}" ;;
  default|ask)                   seg_mode="${C_MODE_ASK}Ask${RESET}" ;;
  "-")                           seg_mode="${DIM}--${RESET}" ;;
  *)                             seg_mode="${C_MODE_PLAN}${cycle_mode}${RESET}" ;;
esac

# 4. Status (nocasematch active)
case "$live_status" in
  idle|ready)                       seg_status="${C_STAT_IDLE}Idle${RESET}" ;;
  working|thinking|streaming|busy)  seg_status="${C_STAT_WORK}Working${RESET}" ;;
  tool_use|tool|running|executing)  seg_status="${C_STAT_TOOL}Tool${RESET}" ;;
  reviewing|waiting|approval)       seg_status="${C_STAT_REV}Review${RESET}" ;;
  "-")                              seg_status="${DIM}--${RESET}" ;;
  *)                                seg_status="${C_STAT_WORK}${live_status}${RESET}" ;;
esac

# 5. Subagents (active/total)
has_active_sub=0
if [[ "$sub_active" =~ ^[0-9]+$ ]] && (( sub_active > 0 )); then
  has_active_sub=1
  seg_sub="${C_SUB}Sub ${sub_active}/${sub_total}${RESET}"
elif [[ "$sub_total" =~ ^[0-9]+$ ]] && (( sub_total > 0 )); then
  seg_sub="${DIM}Sub 0/${sub_total}${RESET}"
else
  seg_sub="${DIM}Sub 0${RESET}"
fi

# 6. Tasks (from brain task.md, falling back to artifact_count)
brain_tasks=$(resolve_brain_tasks "$brain_dir")
if [[ -n "$brain_tasks" ]]; then
  seg_tasks="${C_TASK}Task ${brain_tasks}${RESET}"
elif [[ "$artifact_count" =~ ^[0-9]+$ ]] && (( artifact_count > 0 )); then
  seg_tasks="${C_TASK}Art ${artifact_count}${RESET}"
else
  seg_tasks="${DIM}Task 0/0${RESET}"
fi

# ================= RIGHT SEGMENTS =================

quota_color() {
  if (( $1 <= 10 )); then
    printf '%s' "$RED"
  elif (( $1 <= 30 )); then
    printf '%s' "$YELLOW"
  else
    printf '%s' "$GREEN"
  fi
}

fmt_quota_tier() {
  local label="$1" dec="$2" int_val="$3" mode="$4"
  if [[ "$int_val" == "-" || -z "$int_val" ]]; then
    case "$mode" in
      bar) printf '%s%s [······] --%s' "$DIM" "$label" "$RESET" ;;
      *)   printf '%s%s --%s' "$DIM" "$label" "$RESET" ;;
    esac
    return
  fi

  local color
  color=$(quota_color "$int_val")
  case "$mode" in
    bar) printf '%s%s %s %s%%%s' "$color" "$label" "$(make_bar "$int_val" "$color")" "$dec" "$RESET" ;;
    dec) printf '%s%s %s%%%s' "$color" "$label" "$dec" "$RESET" ;;
    int) printf '%s%s %s%%%s' "$color" "$label" "$int_val" "$RESET" ;;
  esac
}

# Context Window (prefer live transcript.jsonl count when newer than stdin)
pct_int=${ctx_pct%.*}
pct_int=${pct_int:-0}
live_pct=$(resolve_live_ctx_pct "$brain_dir" "$transcript_path" "$ctx_size" "$raw_sid")
if [[ "${live_pct:-}" =~ ^[0-9]+$ ]] && (( live_pct > pct_int )); then
  pct_int=$live_pct
fi

if (( pct_int >= 80 )); then
  ctx_color="$RED"
elif (( pct_int >= 60 )); then
  ctx_color="$YELLOW"
else
  ctx_color="$GREEN"
fi

seg_ctx_bar="${ctx_color}Ctx $(make_bar "$pct_int" "$ctx_color") ${pct_int}%${RESET}"
seg_ctx_num="${ctx_color}Ctx ${pct_int}%${RESET}"

# Emergency single critical metric (lowest quota <= 20%, otherwise Ctx)
if [[ "$h5_int" =~ ^[0-9]+$ ]] && (( h5_int <= 20 )) && { [[ ! "$wk_int" =~ ^[0-9]+$ ]] || (( h5_int <= wk_int )); }; then
  seg_danger=$(fmt_quota_tier "5h" "$h5_dec" "$h5_int" "int")
elif [[ "$wk_int" =~ ^[0-9]+$ ]] && (( wk_int <= 20 )); then
  seg_danger=$(fmt_quota_tier "Wk" "$wk_dec" "$wk_int" "int")
else
  seg_danger="$seg_ctx_num"
fi

# ================= RESPONSIVE SELECTION & ALIGNMENT =================

SEP=" ${DIM}│${RESET} "

left_full="${seg_session}${SEP}${seg_model_full}${SEP}${seg_mode}${SEP}${seg_status}${SEP}${seg_sub}${SEP}${seg_tasks}"
left_split_sub="${seg_model_short}${SEP}${seg_mode}${SEP}${seg_status}${SEP}${seg_sub}"
left_split="${seg_model_short}${SEP}${seg_mode}${SEP}${seg_status}"
left_sidebar="${seg_model_mini}${SEP}${seg_status}"

right_full="$(fmt_quota_tier "Wk" "$wk_dec" "$wk_int" "bar")${SEP}$(fmt_quota_tier "5h" "$h5_dec" "$h5_int" "bar")${SEP}${seg_ctx_bar}"
right_dec="$(fmt_quota_tier "Wk" "$wk_dec" "$wk_int" "dec")${SEP}$(fmt_quota_tier "5h" "$h5_dec" "$h5_int" "dec")${SEP}${seg_ctx_num}"
right_int="$(fmt_quota_tier "Wk" "$wk_dec" "$wk_int" "int")${SEP}$(fmt_quota_tier "5h" "$h5_dec" "$h5_int" "int")${SEP}${seg_ctx_num}"

tty_cols() {
  local tty="$1" dev sz c
  [[ -z "$tty" || "$tty" == "?" || "$tty" == "??" || "$tty" == "-" ]] && return 1
  for dev in "/dev/$tty" "/dev/tty$tty"; do
    if [[ -c "$dev" ]]; then
      sz=$(stty size < "$dev" 2>/dev/null || true)
      c=${sz#* }
      if [[ "$c" =~ ^[0-9]+$ ]] && (( c > 0 )); then
        printf '%s' "$c"
        return 0
      fi
    fi
  done
  return 1
}

get_cols() {
  if [[ "$term_cols" =~ ^[0-9]+$ ]] && (( term_cols > 0 )); then
    printf '%s' "$term_cols"
    return
  fi

  case "${OSTYPE:-}" in
    msys*|cygwin*)
      printf '%s' "${COLUMNS:-140}"
      return
      ;;
  esac

  local pid="${PPID:-}" info parent_pid tty c depth=0
  while [[ -n "$pid" && "$pid" =~ ^[0-9]+$ ]] && (( pid > 1 && depth < 10 )); do
    info=$(ps -p "$pid" -o ppid=,tty= 2>/dev/null || true)
    [[ -z "$info" ]] && break
    read -r parent_pid tty <<< "$info"
    if c=$(tty_cols "$tty"); then
      printf '%s' "$c"
      return
    fi
    pid="$parent_pid"
    depth=$(( depth + 1 ))
  done

  printf '%s' "${COLUMNS:-140}"
}

visible_len() {
  local clean="${1//$'\033'\[*([0-9;])m/}"
  printf '%s' "${#clean}"
}

cols=$(get_cols)

candidates_left=("$left_full" "$left_full")
candidates_right=("$right_full" "$right_dec")

if (( has_active_sub == 1 )); then
  candidates_left+=("$left_split_sub")
  candidates_right+=("$right_dec")
fi

candidates_left+=("$left_split" "$left_sidebar" "$left_sidebar" "$seg_status")
candidates_right+=("$right_dec" "$right_int" "$seg_danger" "$seg_danger")

chosen_left="$seg_status"
chosen_right="$seg_danger"

for (( i=0; i<${#candidates_left[@]}; i++ )); do
  l_cand="${candidates_left[$i]}"
  r_cand="${candidates_right[$i]}"
  l_len=$(visible_len "$l_cand")
  r_len=$(visible_len "$r_cand")
  if (( l_len + r_len + 2 <= cols )); then
    chosen_left="$l_cand"
    chosen_right="$r_cand"
    break
  fi
done

left_len=$(visible_len "$chosen_left")
right_len=$(visible_len "$chosen_right")
pad=$(( cols - left_len - right_len ))

if (( pad >= 2 )); then
  printf -v spacer '%*s' "$pad" ''
else
  spacer="  ${DIM}│${RESET}  "
fi

printf '%s%s%s\n' "$chosen_left" "$spacer" "$chosen_right"
