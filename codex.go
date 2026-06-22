package codegen

import (
	"context"
	"strings"
)

// Codex runs OpenAI's `codex exec` CLI non-interactively.
//
// Base shape:
//
//	codex exec [--json] [-m MODEL] [-s SANDBOX | --dangerously-bypass-...] --skip-git-repo-check -
//
// The prompt is piped on stdin; `-` makes Codex read instructions from stdin.
// Claude-only Config fields (tools, system prompt, MCP config) are ignored.
type Codex struct {
	cfg Config
}

// codexBinary is the executable name resolved on PATH. Var for tests.
var codexBinary = "codex"

// NewCodex constructs a Codex agent. It does not probe PATH; if the binary is
// missing, the first Run call will surface the exec error.
func NewCodex(cfg Config) *Codex {
	return &Codex{cfg: cfg}
}

// Name implements Agent.
func (c *Codex) Name() string { return "codex" }

// Run implements Agent.
func (c *Codex) Run(ctx context.Context, prompt, workDir string, opts ...RunOption) (Result, error) {
	rc := resolveRunConfig(c.cfg, opts)
	return runCLI(ctx, codexBinary, buildCodexArgs(rc, false), prompt, workDir, rc)
}

// buildCodexArgs assembles the argv (sans binary) for a `codex exec` call.
// Streaming forces --json; Codex-specific events are surfaced through
// StreamEvent.Raw without schema normalization.
func buildCodexArgs(rc runConfig, streaming bool) []string {
	args := []string{"exec"}

	if streaming || strings.TrimSpace(rc.outputFormat) == "json" {
		args = append(args, "--json")
	}

	switch sandbox := strings.TrimSpace(rc.sandbox); sandbox {
	case "danger-full-access":
		args = append(args, "--dangerously-bypass-approvals-and-sandbox")
	case "read-only", "workspace-write":
		args = append(args, "-s", sandbox)
	default:
		args = append(args, "-s", "workspace-write")
	}

	args = append(args, "--skip-git-repo-check")

	if model := strings.TrimSpace(rc.model); model != "" {
		args = append(args, "-m", model)
	}

	return append(args, "-")
}
