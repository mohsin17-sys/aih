# FRIDAY (AIH)

**A terminal-based AI coding agent with a hybrid brain: a cloud planner (Mistral) that thinks, a local editor model (Qwen on MLX) that writes, and a routing layer (Jev) that decides who handles what — with you approving every sensitive action.**

FRIDAY reads, writes, and edits files in a real repository, runs shell commands, and reports everything it does live in a terminal UI (TUI), while keeping a full audit trail of every session.

---

## Table of Contents

1. [What FRIDAY does](#what-friday-does)
2. [How it works (the 30-second version)](#how-it-works)
3. [Requirements](#requirements)
4. [Installation](#installation)
5. [Configuration](#configuration)
6. [Usage](#usage)
7. [The approval system (read this!)](#the-approval-system)
8. [The telemetry panel](#the-telemetry-panel)
9. [Branding (custom footer)](#branding)
10. [Maintaining FRIDAY](#maintaining-friday)
11. [Troubleshooting](#troubleshooting)
12. [License](#license)

---

## What FRIDAY does

- **Autonomous coding tasks**: "summarize this repo", "draft a config file", "fix the failing test" — FRIDAY plans multi-step work with tool calls and executes it.
- **File operations**: read, list, and write files (every write requires your approval).
- **Shell commands**: runs commands with a 30-second timeout; read-only commands (like `df -h` or `git status`) run freely, destructive ones are refused outright, everything in between asks first.
- **Local LLM offload**: large file generation (>200 lines) is delegated to a Qwen model running on your own hardware via [mlx-lm](https://github.com/ml-explore/mlx-examples), keeping API costs down.
- **Full audit trail**: every session is written to a `.aih/sessions/*.jsonl` file — every tool call, every result, every approval.

## How it works

```
        your task
            |
            v
   +-----------------+     routing decision      +--------------+
   |  PLANNER        | ---------------------->   | JEV          |
   |  (Mistral API)  |   "which tool handles     | (orchestr.)  |
   |  thinks & plans |    this task?"            +--------------+
   +--------+--------+
            | tool calls
            v
   +----------------------------------------------------------+
   | TOOLS: read_file | list_dir | write_file | run_command   |
   |        generate_edit (-> local Qwen model)               |
   +-----------------------------+----------------------------+
            | sensitive action?
            v
        +------+    approved?    +-----------+
        |  YOU | --------------> | EXECUTION | -> live TUI + JSONL audit
        +------+                 +-----------+
```

## Requirements

- **Go 1.22+** (builds the binary)
- **A Mistral API key** (free tier works) — [console.mistral.ai](https://console.mistral.ai)
- **Optional**: a Mac with Apple Silicon running [mlx-lm](https://github.com/ml-explore/mlx-examples) for the local Qwen editor model. Without it, FRIDAY still works — the planner just writes files directly.
- **Optional**: a Jev orchestrator endpoint for routing decisions. Without it, `jev_route` is disabled and everything still functions.

## Installation

```
# 1. get the code
git clone https://github.com/mohsin17-sys/aih.git
cd aih

# 2. build (installs the binary as ~/bin/friday)
mkdir -p ~/bin
go build -o ~/bin/friday ./cmd/aih

# 3. make sure ~/bin is on your PATH (one-time setup)
echo 'export PATH="/home/mo/bin:/home/mo/bin:/home/mo/bin:/home/mo/bin:/home/mo/bin:/home/mo/.local/bin:/home/mo/bin:/usr/local/bin:/usr/bin"' >> ~/.bashrc   # or ~/.zshrc
source ~/.bashrc

# 4. verify
friday --help
```

## Configuration

FRIDAY reads `~/.config/aih/config.toml`. A full annotated example ships in
[`config.example.toml`](config.example.toml). Quick start:

```
mkdir -p ~/.config/aih
cp config.example.toml ~/.config/aih/config.toml
# then edit: set your planner model; everything else has sane defaults
```

The file looks like this (see the example file for every option explained):

```
[planner]
model = "mistral-large-latest"     # the cloud model that plans

[editor]
base_url = "http://192.168.1.50:8080/v1"   # your mlx-lm server
model = "mlx-community/Qwen2.5-Coder-14B-Instruct-4bit"

[jev]
enabled = false                    # true if you run a Jev orchestrator

[safety]
auto_approve_write = false         # headless mode only (see below)
auto_approve_shell = false
max_cost_usd = 5.0
```

**Warning — important convention:** `editor.base_url` must include the `/v1`
suffix (e.g. `http://host:8080/v1`, not `http://host:8080`). FRIDAY appends
paths like `/chat/completions` and `/models` to this base.

Your Mistral API key comes from the environment:

```
export MISTRAL_API_KEY="..."       # add to ~/.bashrc / ~/.zshrc
```

## Usage

**Interactive TUI (the normal way):**

```
cd ~/my-project
friday
```

Type a task, press Enter. Watch the live transcript: `-> tool args` for calls,
`<- result` for outputs, a `run_command -- allow? [y]es / [n]o` prompt when
something needs approval. Type `exit` to quit.

**Headless (single task, scriptable):**

```
cd ~/my-project
friday -e "read go.mod and tell me the module name"
friday -e "draft a config example file" -steps 20
```

In headless mode there are no interactive prompts: the `[safety]` config
decides what runs (`auto_approve_write` / `auto_approve_shell`, both default
`false` — the agent will state limitations instead of doing sensitive things).

Every run leaves an audit trail in `.aih/sessions/<timestamp>.jsonl` — one
JSON event per line, safe to `grep`, `jq`, or commit to git.

## The approval system

Three safety tiers for shell commands:

| Tier | Examples | Behavior |
|---|---|---|
| **Read-only allowlist** | `ls`, `cat`, `df`, `git status`, `head` | Runs immediately, no prompt |
| **Hard-blocked** | `rm -rf /`, `mkfs`, `dd of=/dev/...`, fork bombs, `shutdown` | **Refused always**, even if you approve |
| **Everything else** | `touch`, `sudo ...`, `git push` | interactive prompt (TUI) or config policy (headless) |

All `write_file` calls require approval in the TUI. Chains are analyzed
per-command: `cat a && rm b` is *not* read-only. Commands time out after
30 seconds. Denied agents are told not to retry and to state the limitation
in their answer.

## The telemetry panel

The right side of the TUI shows live session stats:

- **Tool / Jev / Editor calls, Files changed** — activity counters
- **Tokens in / out** — Mistral planner usage for this session
- **Qwen calls / avg** — local model invocations and mean latency
- **Cost** — set `FRIDAY_PRICE_MTOK=2.0` (dollars per million tokens) before launch to see estimated spend
- **qwen up/down indicator** — health of your local MLX server, re-checked every 15 seconds

## Branding

The TUI footer ("Mo's AI space" by default) is yours to change:

```
# per-invocation
FRIDAY_FOOTER="my workspace" friday

# permanently
echo "my workspace" > ~/.config/friday/footer.txt
```

## Maintaining FRIDAY

**Update to the latest version:**

```
cd aih                              # wherever you cloned it
git pull
go build -o ~/bin/friday ./cmd/aih
```

**Rebuild after changing any Go source:** same `go build` command. The
binary is a single static file; no runtime dependencies beyond your config.

**Check code health before/after hacking on it:**

```
go vet ./...
go build ./...
```

**Where things live (source map):**

| Path | Purpose |
|---|---|
| `cmd/aih/main.go` | entry point, TUI/headless dispatch, session logging |
| `internal/app/app.go` | constructs the agent (tools, config, approvals) |
| `internal/agent/loop.go` | the agent loop, event stream, approval gate |
| `internal/agent/shell.go` | command execution: tiers, timeout, truncation |
| `internal/agent/editor.go` | generate_edit -> local Qwen offload |
| `internal/agent/jevtool.go` | jev_route routing consultation |
| `internal/providers/mistral.go` | planner API client (usage-aware) |
| `internal/providers/mlx.go` | local model client (usage + latency + health) |
| `internal/tui/` | the terminal UI |
| `config/config.go` | config loading (`~/.config/aih/config.toml`) |

**Session audit files** accumulate in each project's `.aih/sessions/`. They
are plain JSONL; delete old ones freely, or add `.aih/` to your project's
`.gitignore` if you don't want them committed.

## Troubleshooting

**`friday: command not found`** — `~/bin` isn't on your PATH (see Installation
step 3), or open a new terminal after editing your rc file.

**`config: ...` error on launch** — your `~/.config/aih/config.toml` has a
TOML syntax error. Compare against `config.example.toml`; common causes: a
missing quote, or a section header like `[planner]` misspelled.

**Mistral calls fail (`mistral HTTP 401`)** — `MISTRAL_API_KEY` isn't set or
has expired. Check: `echo ur1W6utY62J0qvnVMZlUiN2eM0u6dNDN`. In the TUI the key comes from
the environment you launched it from.

**qwen dot shows "down" in the status bar but the model works** — the health
check pings `<base_url>/models`. Remember the convention: `base_url` **must
end in `/v1`**. Test it yourself: `curl -s http://YOUR-HOST:8080/v1/models`
should return a JSON model list within a couple of seconds. If the MLX server
is on another machine (e.g. over Tailscale), make sure it's reachable and not
sleeping.

**`generate_edit` errors like `Post ".../chat/completions": connection refused`**
— the mlx-lm server is down or the URL is wrong. Start it with
`mlx_lm.server --model mlx-community/Qwen2.5-Coder-14B-Instruct-4bit` (on the
Mac serving the model) and verify with the curl above. FRIDAY's planner will
often retry with a rephrased request and recover on its own.

**Qwen generation seems slow** — check `Qwen avg` in the telemetry panel.
Tens of seconds for a 200+ line file is normal for a 14B model on a Mac.
The agent retries failed calls automatically; a single failed attempt
followed by a success shows up as `Editor calls: 2, Qwen calls: 1`.

**Commands "hang"** — they don't; every command dies at 30 seconds and the
result line says `(timed out after 30s; partial output above)`.

**A command I expected to run was refused** — it matched a hard-blocked
pattern (see the approval table). FRIDAY refuses unrecoverable destruction
outright; rephrase the task (e.g. delete a *specific* path, not `/`).

**Text selection / copying in the TUI doesn't work** — hold **Shift** while
dragging to select (bypasses the TUI's keyboard handling in most terminals).
For long outputs you need to copy, use headless mode instead:
`friday -e "..."` prints plain scrolling text.

**Weird characters or clipped output** — long answers are word-wrapped to
your terminal width; resize the window or use a bigger font. If lines look
garbled, ensure your terminal supports UTF-8 and has a Nerd Font for the
arrow/dot glyphs.

**Debugging the TUI itself** — stdout is captured by the TUI, so
`fmt.Println` debugging goes nowhere. Write to a log file instead
(`/tmp/friday-debug.log` is the convention in this project).

**Something else** — check the session JSONL (`.aih/sessions/`) for the exact
event sequence, then file an issue with the relevant lines.

## License

MIT — see [LICENSE](LICENSE).
