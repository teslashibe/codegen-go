# codegen-go

> Drive [Claude Code](https://docs.claude.com/en/docs/claude-code) — and any other code-generation CLI — from Go.

[![ci](https://github.com/teslashibe/codegen-go/actions/workflows/ci.yml/badge.svg)](https://github.com/teslashibe/codegen-go/actions/workflows/ci.yml)
[![go reference](https://pkg.go.dev/badge/github.com/teslashibe/codegen-go.svg)](https://pkg.go.dev/github.com/teslashibe/codegen-go)
[![license: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](./LICENSE)

A tiny, zero-dependency Go library that wraps coding-agent CLIs behind a single
`Agent` interface. Use it to embed Claude Code (or Codex, Aider, OpenHands,
Cline, or any homemade CLI) inside your Go service the same way you'd embed any
other dependency.

```bash
go get github.com/teslashibe/codegen-go
```

---

## Why this exists

Coding-agent CLIs are great at the actual work — reading a repo, planning,
editing files, running tests — but they're awkward to call from a long-lived
service. You end up reinventing:

- piping a multi-megabyte prompt on stdin (so it doesn't hit `ARG_MAX`),
- capping the captured output so a runaway run can't OOM your process,
- propagating context cancellation cleanly through `exec`,
- decoding JSON out of free-form CLI text (with prose and ``` fences around it).

`codegen-go` does all of that in ~600 lines and gives you one interface for
every supported tool, so your service code never has to care which agent the
operator picked.

---

## Quickstart

Make sure the Claude Code CLI is installed and authenticated on whichever host
runs your code:

```bash
npm install -g @anthropic-ai/claude-code
claude login
```

Then:

```go
package main

import (
    "context"
    "fmt"
    "log"

    codegen "github.com/teslashibe/codegen-go"
)

func main() {
    agent := codegen.NewClaudeCode(codegen.Config{
        Model: "claude-sonnet-4-5", // optional; omit to use the CLI default
    })

    res, err := agent.Run(
        context.Background(),
        "Add a README to this directory describing the project.",
        "/path/to/repo",
    )
    if err != nil {
        log.Fatalf("run: %v\n--- agent output (tail) ---\n%s", err, res.Output)
    }
    fmt.Println(res.Output)
}
```

The agent is given a prompt on stdin and a working directory. It is expected
to make any file changes inside `workDir`; you handle `git add` / `git commit`
yourself afterwards (the [`examples/quickstart`](./examples/quickstart) demo
shows the minimal pattern).

---

## Structured outputs

When you use the agent as a reasoner — verifier, reviewer, triager — you want a
typed value back, not free text. `RunJSON` runs the agent and decodes the first
well-formed JSON value in its output:

```go
type Verdict struct {
    Approve bool   `json:"approve"`
    Reason  string `json:"reason"`
}

agent := codegen.NewClaudeCode(codegen.Config{})
var v Verdict
err := codegen.RunJSON(ctx, agent,
    `Should we merge this PR? Respond with {"approve":bool,"reason":string}.`,
    "/path/to/repo",
    &v,
)
```

Markdown code fences (```` ```json ... ``` ````) and surrounding prose are
tolerated. See [`examples/json-reasoner`](./examples/json-reasoner).

---

## Swap in another CLI

Anything that reads a prompt from stdin and edits files in its working
directory works. Construct a `GenericCLI` with the binary name and any flags
the tool needs to run non-interactively:

```go
codex := codegen.NewGenericCLI(codegen.Config{
    Command: "codex",
    Args:    []string{"--auto-approve"},
})

aider := codegen.NewGenericCLI(codegen.Config{
    Command: "aider",
    Args:    []string{"--yes", "--no-stream", "--message-file", "-"},
})
```

Or pick one at runtime via `NewAgent` — pass `Type: "claude-code"` or
`Type: "generic"` (with `Command`/`Args`). See
[`examples/with-codex`](./examples/with-codex).

| Preset | `Type` | Notes |
|---|---|---|
| Claude Code | `claude-code` (default) | Anthropic's `claude` CLI; needs `claude login`. |
| OpenAI Codex | `generic` | `Command: "codex", Args: ["--auto-approve"]` |
| Aider | `generic` | `Command: "aider", Args: ["--yes","--no-stream","--message-file","-"]` |
| OpenHands | `generic` | `Command: "openhands"`, plus your install's non-interactive flags |
| Cline | `generic` | `Command: "cline"` (via the Cline CLI shim) |
| Custom | `generic` | Any binary that reads a prompt from stdin and edits files in `cwd` |

---

## Configuration

Everything is in [`Config`](./agent.go) and per-call [`RunOption`](./agent.go).
`RunOption` always wins over `Config`; `Config` defaults apply when a field is
zero.

| Field | Default | Purpose |
|---|---|---|
| `Type` | `"claude-code"` | Implementation selector for `NewAgent`. `"claude-code"` or `"generic"`. |
| `Model` | (CLI default) | `--model` value passed to `claude`. Ignored by `GenericCLI`. |
| `Timeout` | `30m` (`DefaultTimeout`) | Per-`Run` cap. Non-positive disables; the parent `ctx` still applies. |
| `MaxOutputBytes` | `10 MiB` (`DefaultMaxOutputBytes`) | Cap on captured combined stdout/stderr. Negative disables. |
| `Command` | — | Binary for `GenericCLI`. |
| `Args` | — | Extra argv prepended for `GenericCLI`. |

```go
res, err := agent.Run(ctx, prompt, workDir,
    codegen.WithModel("claude-opus-4-5"),
    codegen.WithTimeout(45*time.Minute),
    codegen.WithMaxOutputBytes(50<<20),
)
```

---

## Result and exit codes

```go
type Result struct {
    Output    string         // combined stdout + stderr, capped at MaxOutputBytes
    ExitCode  int            // 0 success, the OS exit code on failure, -1 if the process never started or ctx cancelled it
    Duration  time.Duration  // wall-clock
    Truncated bool           // true if Output was clipped to fit MaxOutputBytes
}
```

Errors wrap `*exec.ExitError` so you can `errors.As` if you care about the
distinction between a clean non-zero exit and a fork/exec failure. The captured
`Output` is always populated, even on error — surface its tail in your logs to
debug stuck agents:

```go
res, err := agent.Run(ctx, prompt, workDir)
if err != nil {
    tail := res.Output
    if len(tail) > 4000 {
        tail = tail[len(tail)-4000:]
    }
    log.Printf("%s failed (exit=%d): %v\n--- output tail ---\n%s",
        agent.Name(), res.ExitCode, err, tail)
}
```

---

## Sandbox

The wrapper sets `cmd.Dir = workDir` and pipes the prompt over stdin. That is
the **only** sandboxing primitive — the agent sees the same environment your
process does, with full read/write to the host filesystem. If you're running
untrusted prompts, run them in a container or VM. A common pattern is per-task
git worktrees so each agent run only sees one branch's worth of code.

---

## In production

`codegen-go` was extracted from [Eva
Board](https://github.com/EvaEverywhere/eva-board), where it drives an
autonomous agent loop: code → verify → review → retry → open PR. Every endpoint
in this README is exercised in that loop on every card.

---

## API

Full reference at [pkg.go.dev](https://pkg.go.dev/github.com/teslashibe/codegen-go).
Everything you need:

```go
type Agent interface {
    Name() string
    Run(ctx context.Context, prompt, workDir string, opts ...RunOption) (Result, error)
}

func NewAgent(cfg Config) (Agent, error)        // factory ("claude-code" | "generic")
func NewClaudeCode(cfg Config) *ClaudeCode      // direct
func NewGenericCLI(cfg Config) *GenericCLI      // direct

func WithModel(model string) RunOption
func WithTimeout(d time.Duration) RunOption
func WithMaxOutputBytes(n int) RunOption

func RunJSON(ctx context.Context, a Agent, prompt, workDir string, out any, opts ...RunOption) error
```

---

## MCP support

This package ships an [MCP](https://modelcontextprotocol.io/) tool surface in
`./mcp` for use with [`teslashibe/mcptool`](https://github.com/teslashibe/mcptool)-compatible
hosts (e.g. [`teslashibe/agent-setup`](https://github.com/teslashibe/agent-setup)).
Two tools cover the full `Agent` surface:

- `codegen_run` — execute the configured coding-agent CLI (Claude Code or any
  generic CLI) with a prompt against a working directory; returns the
  captured combined stdout/stderr, exit code, duration, and a truncation flag.
- `codegen_run_json` — same as `codegen_run` but uses `RunJSON` to decode the
  first well-formed JSON object/array from the agent's output, so you can use
  the agent as a structured reasoner (verifier, reviewer, triager).

```go
import (
    "github.com/teslashibe/mcptool"
    codegen "github.com/teslashibe/codegen-go"
    cgmcp "github.com/teslashibe/codegen-go/mcp"
)

agent, _ := codegen.NewAgent(codegen.Config{Type: "claude-code"})
provider := cgmcp.Provider{}
for _, tool := range provider.Tools() {
    // register tool with your MCP server, passing agent as the
    // opaque client argument when invoking
}
```

A coverage test in `mcp/mcp_test.go` fails if a new exported method is added
to `codegen.Agent` without either being wrapped by an MCP tool or being added
to `mcp.Excluded` with a reason — keeping the MCP surface in lockstep with
the package API is enforced by CI rather than convention.

`codegen_run` is the most powerful tool in any MCP inventory: it executes a
CLI inside the host process with full filesystem access. Gate it behind
explicit user consent, run untrusted prompts in a sandbox (container, VM, or
ephemeral git worktree), and use `timeout_seconds` / `max_output_bytes` to
keep individual runs bounded.

## License

MIT — see [LICENSE](./LICENSE).
