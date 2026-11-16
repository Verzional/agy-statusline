#!/usr/bin/env bash
set -euo pipefail

case "$(uname -s 2>/dev/null || echo Unknown)" in
  Darwin) export LC_ALL=en_US.UTF-8 2>/dev/null || true ;;
  *)      export LC_ALL=C.UTF-8 2>/dev/null || export LC_ALL=en_US.UTF-8 2>/dev/null || true ;;
esac

input=$(cat)

# --- ANSI Colors (Distinct Per Segment) ---
C_SESSION='\033[38;5;141m'   # Lavender
C_MODEL='\033[38;5;210m'     # Coral
C_MODE_PLAN='\033[38;5;117m' # Sky
C_MODE_EDIT='\033[38;5;221m' # Warm Gold
C_MODE_AUTO='\033[38;5;212m' # Pink
C_MODE_ASK='\033[38;5;156m'  # Lime
C_STAT_IDLE='\033[38;5;114m' # Mint
C_STAT_WORK='\033[38;5;208m' # Tangerine
C_STAT_TOOL='\033[38;5;80m'  # Turquoise
C_STAT_REV='\033[38;5;176m'  # Orchid
C_SUB='\033[38;5;81m'        # Electric Cyan
C_TASK='\033[38;5;229m'      # Warm Cream

GREEN='\033[32m'
YELLOW='\033[33m'
RED='\033[31m'
DIM='\033[2m'
RESET='\033[0m'

# --- Single-Pass JSON Extraction ---
IFS=$'\t' read -r \
  term_cols raw_sid payload_title transcript_path artifact_count \
  model_id model_name effort \
  g_wk g_5h a_wk p_wk p_5h \
  ctx_pct ctx_size cycle_mode live_status sub_active sub_total < <(
  echo "$input" | jq -r '[
    (.terminal_width // .terminal.columns // .dimensions.columns // .viewport.width // .columns // .width // 0),
    (.conversation_id // .session_id // "-"),
    (.conversation_title // .session_title // .title // "-"),
    (.transcript_path // "-"),
    (.artifact_count // 0),
    (if (.model | type) == "object" then (.model.id // "-") else "-" end),
    (if (.model | type) == "object" then (.model.display_name // .model.name // .model.id // "default") else (.model // "default") end),
    (if (.model | type) == "object" then (.model.effort // "-") else "-" end),
    (.quota["gemini-weekly"].remaining_fraction // "-"),
    (.quota["gemini-5h"].remaining_fraction // "-"),
    (.quota["gemini-argon-wtu-weekly"].remaining_fraction // "-"),
    (.quota["3p-weekly"].remaining_fraction // "-"),
    (.quota["3p-5h"].remaining_fraction // "-"),
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
  ] | map(tostring | gsub("[\t\r\n]+"; " ")) | @tsv'
)

# --- Resolve Brain Directory ---
resolve_brain_dir() {
  local sid="$1"
  local tpath="$2"
  local candidate

  for candidate in \
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

# --- Live Context Usage from transcript.jsonl ---
resolve_live_ctx_pct() {
  local bdir="$1"
  local tpath="$2"
  local max_tokens="$3"
  local log_file=""

  if [[ -n "$bdir" && -f "${bdir}/.system_generated/logs/transcript.jsonl" ]]; then
    log_file="${bdir}/.system_generated/logs/transcript.jsonl"
  elif [[ "$tpath" != "-" && -f "$tpath" ]]; then
    log_file="$tpath"
  fi

  [[ -z "$log_file" || ! "$max_tokens" =~ ^[0-9]+$ || "$max_tokens" -le 0 ]] && return

  local used_tokens
  used_tokens=$(tail -n 30 "$log_file" 2>/dev/null | jq -R '
    fromjson? | select(type == "object" and (.input_tokens != null or .cache_read_tokens != null)) |
    ((.input_tokens // 0) + (.cache_read_tokens // 0) + (.output_tokens // 0))
  ' 2>/dev/null | tail -n 1 || true)

  if [[ "$used_tokens" =~ ^[0-9]+$ ]] && (( used_tokens > 0 )); then
    echo $(( used_tokens * 100 / max_tokens ))
  fi
}

# --- Fallback Session Title Lookup ---
resolve_session_title() {
  local sid="$1"
  local meta="$HOME/.gemini/antigravity-cli/cache/conversation_metadata.json"
  local cache="${TMPDIR:-/tmp}/agy-title-${sid}"

  if [[ -f "$cache" && -f "$meta" && ! "$meta" -nt "$cache" ]]; then
    cat "$cache"
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

# --- Resolve Brain Tasks (task.md checkboxes) ---
resolve_brain_tasks() {
  local bdir="$1"
  local task_file="${bdir}/task.md"
  if [[ -n "$bdir" && -f "$task_file" ]]; then
    awk '
      /^[[:space:]]*[-*][[:space:]]+\[[xX]\]/ { done++; total++ }
      /^[[:space:]]*[-*][[:space:]]+\[[[:space:]]\]/ { total++ }
      END { if (total > 0) printf "%d/%d", done+0, total }
    ' "$task_file" 2>/dev/null || true
  fi
}

# --- Shared Bar Helper (UTF-8 Safe on BSD & GNU) ---
repeat_str() {
  local str="$1" count="$2" out="" i
  for (( i=0; i<count; i++ )); do
    out+="$str"
  done
  printf '%s' "$out"
}

make_bar() {
  local pct_int="$1"
  local color="$2"
  local width=6
  (( pct_int < 0 )) && pct_int=0
  (( pct_int > 100 )) && pct_int=100
  local filled=$(( pct_int * width / 100 ))
  (( pct_int > 0 && filled == 0 )) && filled=1
  local empty=$(( width - filled ))
  local f_str e_str
  f_str=$(repeat_str "█" "$filled")
  e_str=$(repeat_str "·" "$empty")
  printf "${color}[%s${DIM}%s${RESET}${color}]" "$f_str" "$e_str"
}

# ================= LEFT CLUSTER (AGENT & EXECUTION) =================

# 1. Session Name
session_label=""
if [[ "$payload_title" != "-" && -n "$payload_title" && "$payload_title" != "$raw_sid" ]]; then
  session_label="$payload_title"
elif [[ "$raw_sid" != "-" && -n "$raw_sid" ]]; then
  session_label=$(resolve_session_title "$raw_sid")
fi

if [[ -n "$session_label" ]]; then
  seg_session="${C_SESSION}${session_label}${RESET}"
elif [[ "$raw_sid" != "-" && -n "$raw_sid" ]]; then
  seg_session="${DIM}${raw_sid:0:8}${RESET}"
else
  seg_session="${DIM}--${RESET}"
fi

# 2. Model Name & Effort
if [[ "$effort" != "-" && -n "$effort" ]]; then
  effort_cap=$(printf '%s' "$effort" | awk '{print toupper(substr($0,1,1)) tolower(substr($0,2))}')
  model_lc=$(printf '%s' "$model_name" | tr '[:upper:]' '[:lower:]')
  effort_lc=$(printf '%s' "$effort" | tr '[:upper:]' '[:lower:]')
  if [[ "$model_lc" != *"(${effort_lc})"* ]]; then
    model_name="${model_name} (${effort_cap})"
  fi
fi
seg_model="${C_MODEL}${model_name}${RESET}"

# 3. Mode
mode_lc=$(printf '%s' "$cycle_mode" | tr '[:upper:]' '[:lower:]')
case "$mode_lc" in
  plan)                          seg_mode="${C_MODE_PLAN}Plan${RESET}" ;;
  accept-edits|acceptedits|edit) seg_mode="${C_MODE_EDIT}Edits${RESET}" ;;
  auto|yolo|bypass*)             seg_mode="${C_MODE_AUTO}Auto${RESET}" ;;
  default|ask)                   seg_mode="${C_MODE_ASK}Ask${RESET}" ;;
  "-")                           seg_mode="${DIM}--${RESET}" ;;
  *)                             seg_mode="${C_MODE_PLAN}${cycle_mode}${RESET}" ;;
esac

# 4. Status
status_lc=$(printf '%s' "$live_status" | tr '[:upper:]' '[:lower:]')
case "$status_lc" in
  idle|ready)                       seg_status="${C_STAT_IDLE}Idle${RESET}" ;;
  working|thinking|streaming|busy)  seg_status="${C_STAT_WORK}Working${RESET}" ;;
  tool_use|tool|running|executing)  seg_status="${C_STAT_TOOL}Tool${RESET}" ;;
  reviewing|waiting|approval)       seg_status="${C_STAT_REV}Review${RESET}" ;;
  "-")                              seg_status="${DIM}--${RESET}" ;;
  *)                                seg_status="${C_STAT_WORK}${live_status}${RESET}" ;;
esac

# 5. Subagents (active/total)
if [[ "$sub_active" =~ ^[0-9]+$ ]] && (( sub_active > 0 )); then
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

# ================= RIGHT CLUSTER (ALL USAGE DATA) =================

full_model_lc=$(printf '%s %s' "$model_id" "$model_name" | tr '[:upper:]' '[:lower:]')
if [[ "$full_model_lc" == *"claude"* || "$full_model_lc" == *"gpt"* ]]; then
  wk_frac="$p_wk"
  h5_frac="$p_5h"
elif [[ "$full_model_lc" == *"argon"* ]]; then
  wk_frac="$a_wk"
  h5_frac="$g_5h"
else
  wk_frac="$g_wk"
  h5_frac="$g_5h"
fi

fmt_quota_bar() {
  local label="$1"
  local frac="$2"
  if [[ "$frac" == "-" || -z "$frac" ]]; then
    printf "${DIM}%s [······] --${RESET}" "$label"
    return
  fi
  local pct pct_int color="$GREEN"
  pct=$(awk -v f="$frac" 'BEGIN { printf "%.2f", f * 100 }')
  pct_int=${pct%.*}
  if (( pct_int <= 10 )); then
    color="$RED"
  elif (( pct_int <= 30 )); then
    color="$YELLOW"
  fi
  local bar
  bar=$(make_bar "$pct_int" "$color")
  printf "${color}%s %b %s%%${RESET}" "$label" "$bar" "$pct"
}

# 7 & 8. Weekly & 5-Hour Quotas
seg_wk=$(fmt_quota_bar "Wk" "$wk_frac")
seg_5h=$(fmt_quota_bar "5h" "$h5_frac")

# 9. Context Window (prefer live transcript.jsonl count when newer than stdin)
pct_int=${ctx_pct%.*}
pct_int=${pct_int:-0}
live_pct=$(resolve_live_ctx_pct "$brain_dir" "$transcript_path" "$ctx_size")
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
ctx_bar=$(make_bar "$pct_int" "$ctx_color")
seg_ctx="${ctx_color}Ctx ${ctx_bar} ${pct_int}%${RESET}"

# ================= ASSEMBLE & ALIGN =================

SEP=" ${DIM}│${RESET} "
left_block="${seg_session}${SEP}${seg_model}${SEP}${seg_mode}${SEP}${seg_status}${SEP}${seg_sub}${SEP}${seg_tasks}"
right_block="${seg_wk}${SEP}${seg_5h}${SEP}${seg_ctx}"

tty_cols() {
  local tty="$1" dev sz c
  [[ -z "$tty" || "$tty" == "?" || "$tty" == "??" || "$tty" == "-" ]] && return 1
  for dev in "/dev/$tty" "/dev/tty$tty"; do
    if [[ -c "$dev" ]]; then
      sz=$(stty size < "$dev" 2>/dev/null || true)
      c=${sz#* }
      if [[ "$c" =~ ^[0-9]+$ ]] && (( c > 0 )); then
        echo "$c"
        return 0
      fi
    fi
  done
  return 1
}

get_cols() {
  if [[ "$term_cols" =~ ^[0-9]+$ ]] && (( term_cols > 0 )); then
    echo "$term_cols"
    return
  fi

  case "${OSTYPE:-}" in
    msys*|cygwin*)
      echo "${COLUMNS:-140}"
      return
      ;;
  esac

  local pid="${PPID:-}" info parent_pid tty c depth=0
  while [[ -n "$pid" && "$pid" =~ ^[0-9]+$ ]] && (( pid > 1 && depth < 10 )); do
    info=$(ps -p "$pid" -o ppid=,tty= 2>/dev/null || true)
    [[ -z "$info" ]] && break
    read -r parent_pid tty <<< "$info"
    if c=$(tty_cols "$tty"); then
      echo "$c"
      return
    fi
    pid="$parent_pid"
    depth=$(( depth + 1 ))
  done

  echo "${COLUMNS:-140}"
}

visible_len() {
  local clean
  clean=$(printf '%b' "$1" | sed $'s/\033\\[[0-9;]*[mK]//g')
  printf '%s' "$clean" | wc -m | tr -d ' '
}

cols=$(get_cols)
left_len=$(visible_len "$left_block")
right_len=$(visible_len "$right_block")
pad=$(( cols - left_len - right_len ))

if (( pad >= 2 )); then
  spacer=$(printf "%${pad}s")
else
  spacer="  ${DIM}│${RESET}  "
fi

printf "%b%b%b\n" "$left_block" "$spacer" "$right_block"
