package codegen

import (
	"context"
	"strings"
)

// ClaudeCode runs Anthropic's `claude` CLI in non-interactive print mode.
//
// The base command shape is:
//
//	claude -p --output-format <fmt> --dangerously-skip-permissions [--model M]
//
// Additional flags are appended when the corresponding Config / RunOption
// fields are set: --append-system-prompt, --mcp-config, --allowedTools,
// --disallowedTools, and --verbose (forced when Stream is used).
//
// The prompt is piped on stdin rather than passed as an argv element. Large
// prompts (e.g. board triage with many cards) can otherwise exceed the
// kernel's ARG_MAX limit (~256KB on macOS) and fail fork/exec with
// "argument list too long".
type ClaudeCode struct {
	cfg Config
}

// claudeBinary is the executable name resolved on PATH. Var for tests.
var claudeBinary = "claude"

// NewClaudeCode constructs a ClaudeCode agent. It does not probe PATH; if the
// binary is missing, the first Run call will surface the exec error.
func NewClaudeCode(cfg Config) *ClaudeCode {
	return &ClaudeCode{cfg: cfg}
}

// Name implements Agent.
func (c *ClaudeCode) Name() string { return "claude-code" }

// Run implements Agent.
func (c *ClaudeCode) Run(ctx context.Context, prompt, workDir string, opts ...RunOption) (Result, error) {
	rc := resolveRunConfig(c.cfg, opts)
	return runCLI(ctx, claudeBinary, buildClaudeArgs(rc, false), prompt, workDir, rc)
}

// buildClaudeArgs assembles the argv (sans binary name) for a `claude` call
// from the resolved runConfig. The streaming flag forces --verbose +
// --output-format stream-json regardless of rc.outputFormat, since both are
// required for Stream() to receive structured events.
func buildClaudeArgs(rc runConfig, streaming bool) []string {
	args := []string{"-p"}

	format := strings.TrimSpace(rc.outputFormat)
	switch {
	case streaming:
		args = append(args, "--output-format", "stream-json", "--verbose")
	case format == "":
		args = append(args, "--output-format", "text")
	default:
		args = append(args, "--output-format", format)
	}

	args = append(args, "--dangerously-skip-permissions")

	if model := strings.TrimSpace(rc.model); model != "" {
		args = append(args, "--model", model)
	}
	if sp := strings.TrimSpace(rc.appendSystemPrompt); sp != "" {
		args = append(args, "--append-system-prompt", sp)
	}
	if mcp := strings.TrimSpace(rc.mcpConfigPath); mcp != "" {
		args = append(args, "--mcp-config", mcp)
	}
	if joined := joinNonEmpty(rc.allowedTools); joined != "" {
		args = append(args, "--allowedTools", joined)
	}
	if joined := joinNonEmpty(rc.disallowedTools); joined != "" {
		args = append(args, "--disallowedTools", joined)
	}
	return args
}

// joinNonEmpty trims and comma-joins tool patterns, dropping empty entries.
// claude-code accepts comma-separated patterns to --allowedTools.
func joinNonEmpty(in []string) string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if t := strings.TrimSpace(s); t != "" {
			out = append(out, t)
		}
	}
	return strings.Join(out, ",")
}
