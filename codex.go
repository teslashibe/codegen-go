package codegen

import (
	"context"
	"strings"
)

// Codex runs OpenAI's `codex exec` CLI non-interactively.
//
// Base shape:
//
//	codex exec [--json] [-s SANDBOX | --dangerously-bypass-...] --skip-git-repo-check [-m MODEL] -
//
// The prompt is piped on stdin; `-` makes Codex read instructions from stdin
// rather than taking them as a positional argument. Piping (like the claude
// preset) avoids the kernel's ARG_MAX limit on large prompts.
//
// Unlike claude-code, Codex uses the `exec` subcommand (not a `-p` flag) and
// a sandbox policy (`-s <mode>` / --dangerously-bypass-approvals-and-sandbox)
// for permissioning. The claude-only Config fields (AppendSystemPrompt,
// MCPConfigPath, AllowedTools, DisallowedTools) have no Codex equivalent and
// are silently ignored — matching how the package already treats cross-preset
// fields.
type Codex struct {
	cfg Config
}

// codexBinary is the executable name resolved on PATH. Var for tests.
var codexBinary = "codex"

// NewCodex constructs a Codex agent. It does not probe PATH; if the binary is
// missing, the first Run call will surface the exec error.
func NewCodex(cfg Config) *Codex { return &Codex{cfg: cfg} }

// Name implements Agent.
func (c *Codex) Name() string { return "codex" }

// Run implements Agent.
func (c *Codex) Run(ctx context.Context, prompt, workDir string, opts ...RunOption) (Result, error) {
	rc := resolveRunConfig(c.cfg, opts)
	return runCLI(ctx, codexBinary, buildCodexArgs(rc, false), prompt, workDir, rc)
}

// buildCodexArgs assembles the argv (sans binary name) for a `codex exec`
// call from the resolved runConfig. The streaming flag forces `--json` (JSONL
// events) regardless of rc.outputFormat, since Stream() needs structured
// events on stdout.
func buildCodexArgs(rc runConfig, streaming bool) []string {
	args := []string{"exec"}

	if streaming {
		args = append(args, "--json")
	} else if f := strings.TrimSpace(rc.outputFormat); f == "json" {
		args = append(args, "--json")
	}

	// Permission/sandbox: explicit Sandbox wins; otherwise default to a
	// writable-but-bounded policy suitable for autonomous edits.
	switch strings.TrimSpace(rc.sandbox) {
	case "danger-full-access":
		args = append(args, "--dangerously-bypass-approvals-and-sandbox")
	case "read-only", "workspace-write":
		args = append(args, "-s", rc.sandbox)
	default:
		args = append(args, "-s", "workspace-write")
	}

	args = append(args, "--skip-git-repo-check")

	if m := strings.TrimSpace(rc.model); m != "" {
		args = append(args, "-m", m)
	}

	// Read the prompt from stdin (runCLI pipes it there). `-` is explicit.
	args = append(args, "-")
	return args
}
