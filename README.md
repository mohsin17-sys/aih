# FRIDAY (AIH)

**A terminal-based AI coding agent with a hybrid brain: a planner/architect model that thinks (any OpenAI-compatible server — e.g. mlx-serve on a Mac mini, Ollama, llama.cpp, or the Mistral API), a local editor model that writes, and a routing layer (Jev) that decides who handles what — with you approving every sensitive action.**

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
- **Local LLM offload**: large file generation (>200 lines) is delegated to a local editor model on your own hardware (e.g. Qwen via Ollama or MLX), keeping costs down and everything private.
- **Full audit trail**: every session is written to a `.aih/sessions/*.jsonl` file — every tool call, every result, every approval.

## How it works

```
        your task
            |
            v
   +-----------------+     routing decision      +--------------+
   |  PLANNER /      | ---------------------->   | JEV          |
   |  ARCHITECT      |   "which tool handles     | (orchestr.)  |
   |  (OpenAI-compat |    this task?")           +--------------+
   |  server)        |
   |  thinks & plans |
   +--------+--------+
            | tool calls
            v
   +----------------------------------------------------------+
   | TOOLS: read_file | list_dir | write_file | run_command   |
   |        generate_edit (-> local editor model)             |
   +-----------------------------+----------------------------+
            | sensitive action?
            v
        +------+    approved?    +-----------+
        |  YOU | --------------> | EXECUTION | -> live TUI + JSONL audit
        +------+                 +-----------+
```

## Requirements

- **Go 1.22+** (builds the binary)
- **A planner that speaks the OpenAI tool-calling protocol** — pick one:
  - any local OpenAI-compatible server: [mlx-serve](https://pypi.org/project/mlx-serve/) on a Mac (e.g. `Josiefied-Qwen3-8B-abliterated-v1-4bit`), Ollama, llama.cpp `llama-server`, vLLM, ...
  - or the Mistral API (free tier works) with a `MISTRAL_API_KEY` — [console.mistral.ai](https://console.mistral.ai)
- **Optional**: a local editor model server for `generate_edit` (e.g. `ollama pull qwen3:4b-instruct`). Without it, FRIDAY still works — the planner just writes files directly.
- **Optional**: a System One routing endpoint for `jev_route` decisions. Use a **local [Laya](https://github.com/NandhaKishorM/laya) server** (`pip install "laya[serve]"` → `laya-serve`; free, private, no key — set `[jev].base_url = "http://127.0.0.1:8000/v1/systemone"`) or hosted TypeSafe Jev (`TYPESAFE_API_KEY`). Without either, `jev_route` is disabled and everything still functions.

## Installation

```
# 1. get the code
git clone https://github.com/mohsin17-sys/aih.git
cd aih

# 2. build (installs the binary as ~/bin/friday)
mkdir -p ~/bin
go build -o ~/bin/friday ./cmd/aih

# 3. make sure ~/bin is on your PATH (one-time setup)
echo 'export PATH="$HOME/bin:$PATH"' >> ~/.bashrc   # or ~/.zshrc
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
# LOCAL planner (e.g. mlx-serve on a Mac mini) — any OpenAI-compatible
# server with tool-call support. base_url MUST include /v1.
provider = "openai"
base_url = "https://mos-mac-mini.tail2f1a37.ts.net/v1"
model = "mtplx-qwen35-9b-optimized-speed"

# ...or leave base_url empty for the Mistral API:
# model = "mistral-large-latest"  (uses MISTRAL_API_KEY from the env)

[editor]
base_url = "http://127.0.0.1:11434/v1"     # e.g. Ollama on this machine
model = "qwen3:4b-instruct"

[jev]
enabled = false                    # true if you run a Jev orchestrator

[safety]
auto_approve_write = false         # headless mode only (see below)
auto_approve_shell = false
max_cost_usd = 5.0
```

**Warning — important convention:** `planner.base_url` and `editor.base_url`
must include the `/v1` suffix (e.g. `http://host:8080/v1`, not `http://host:8080`).
FRIDAY appends paths like `/chat/completions` and `/models` to this base.

If you use the Mistral cloud fallback, the key comes from the environment:

```
export MISTRAL_API_KEY="..."       # add to ~/.bashrc / ~/.zshrc
```

### MCP servers

Each `[[mcp.servers.NAME]]` entry in config spawns an MCP server over stdio
(MCP servers are arbitrary configured code — see the approval table for how
they are gated). FRIDAY exposes *all* connected servers through two tools
only — `mcp_list_tools` and `mcp_call` — so the tool definitions sent to the
planner stay tiny no matter how many tools the servers advertise. Tool
listings are live: servers that swap their toolset mid-session (Serena
reveals its REPL after project activation) are re-queried on every
`mcp_list_tools`, and `tools/list_changed` notifications refresh the cache automatically.

```
[mcp.servers.context7]
command = "npx"
args = ["-y", "@upstash/context7-mcp"]
trusted = true        # no per-call approval for this server
enabled = true        # optional, default true; set false to skip the
                      # server entirely (not spawned, not exposed)
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

**Queue a prompt while FRIDAY is working.** You don't have to wait for the
current task to finish before typing the next one:

- Keep typing while a task runs, then press **Enter** to **queue** that prompt.
  It runs automatically as soon as the current task ends. Queue as many as you
  like; the status line shows the count.
- Press **Enter on an empty line** to **interrupt now** — FRIDAY pauses the
  current task at its next step and immediately starts the queued prompt.

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

`safety.auto_approve_all = true` in config switches FRIDAY into fully
autonomous mode: no approval prompts anywhere — not in the TUI, not
headless. Writes, shell commands, and calls to untrusted MCP servers all
execute; each session's audit trail (`.aih/sessions/*.jsonl`) is the
record. In the TUI the status line shows an `auto` badge so you always
know the gates are off. In the TUI you can also flip it live at any time
— even mid-task — with **F2** or by typing **`/auto`**; the change applies
to every approval decision from that moment on. The only thing still
refused in every mode is the hard-blocked destruction list below.

Otherwise, three safety tiers for shell commands:

| Tier | Examples | Behavior |
|---|---|---|
| **Read-only allowlist** | `ls`, `cat`, `df`, `git status`, `head` | Runs immediately, no prompt |
| **Hard-blocked** | `rm -rf /`, `mkfs`, `dd of=/dev/...`, fork bombs, `shutdown` | **Refused always**, even if you approve |
| **Everything else** | `touch`, `sudo ...`, `git push` | interactive prompt (TUI) or config policy (headless) |

All `write_file` calls require approval in the TUI. Chains are analyzed
per-command: `cat a && rm b` is *not* read-only. Commands containing
output redirection (`>`, `>>`) are never treated as read-only —
`echo x >> file` asks first like any other state-changing command.
Commands time out after
30 seconds. Denied agents are told not to retry and to state the limitation
in their answer.

## The telemetry panel

The right side of the TUI shows live session stats:

- **Tool / Jev / Editor calls, Files changed** — activity counters
- **MCP up/dn** — connected MCP servers with tool counts, and any server that failed to start
- **Tokens in / out** — planner model usage for this session
- **Editor gens / avg** — local editor-model invocations and mean latency
- **Cost** — set `FRIDAY_PRICE_MTOK=2.0` (dollars per million tokens) before launch to see estimated spend (irrelevant for local models)
- **editor up/down indicator** — health of your local editor server, re-checked every 15 seconds

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
| `internal/agent/editor.go` | generate_edit -> local editor-model offload |
| `internal/agent/jevtool.go` | jev_route routing consultation |
| `internal/providers/mistral.go` | planner chat client (Mistral API or any OpenAI-compatible server, usage-aware + retrying) |
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
has expired. Check: `echo "$MISTRAL_API_KEY"` prints a value in the
environment FRIDAY was launched from (a renamed terminal won't inherit rc
edits). If a key was ever pasted into a file, log, or chat — rotate it in
the Mistral console and treat the old one as burned.

**Planner errors (`planner HTTP ...` or connection refused)** — the planner
`base_url` is wrong, the server is down, or it doesn't speak the
OpenAI-compatible tool-call protocol. Test: `curl -s <base_url>/models`
should return a JSON model list; a `tool_calls` response from
`/chat/completions` is required for the agent loop. For mlx-serve on a
Mac mini, make sure it listens on an interface reachable from this
machine (Tailscale or LAN) and isn't asleep.

**Planner steps time out (`planner: ... context deadline exceeded`)** —
FRIDAY streams planner responses (SSE), so a thinking model that reasons
for minutes keeps the connection alive instead of stalling on the full
response; the only deadline is the per-step cap of 15 minutes. Raise or
disable it with `FRIDAY_PLANNER_TIMEOUT=<seconds>` (`0` = unlimited).
Whatever happens is recorded in the session JSONL as a
`{"type":"error","tool":"planner"}` line.

**editor dot shows "down" in the status bar but the model works** — the health
check pings `<base_url>/models`. Remember the convention: `base_url` **must
end in `/v1`**. Test it yourself: `curl -s http://127.0.0.1:11434/v1/models`
(for Ollama) should return a JSON model list within a couple of seconds.

**`generate_edit` errors like `Post ".../chat/completions": connection refused`**
— the editor server is down or the URL is wrong. For Ollama:
`sudo systemctl enable --now ollama` (or `ollama serve`), and
`ollama pull qwen3:4b-instruct` once. FRIDAY's planner will often retry
with a rephrased request and recover on its own.

**Editor generation seems slow** — check `Editor avg` in the telemetry
panel. Tens of seconds for a 200+ line file is normal for a small local
model. The agent retries failed calls automatically; a single failed
attempt followed by a success shows up as
`Editor calls: 2, Editor gens: 1`.

**Commands "hang"** — they don't; every command dies at 30 seconds and the
result line says `(timed out after 30s; partial output above)`.

**A command I expected to run was refused** — it matched a hard-blocked
pattern (see the approval table). FRIDAY refuses unrecoverable destruction
outright; rephrase the task (e.g. delete a *specific* path, not `/`).

**Text selection / copying in the TUI** — drag with the left button to
select (release to copy to the clipboard via OSC 52; tmux is handled).
Hold the button and use the scroll wheel to extend the selection past the
visible screen — the view scrolls with it, like a native terminal. Hold
**Shift** while dragging instead to use your terminal emulator's own
selection. For long outputs you need to copy, headless mode also works:
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
