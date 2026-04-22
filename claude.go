package codegen

import (
	"context"
	"strings"
)

// ClaudeCode runs Anthropic's `claude` CLI in non-interactive print mode.
//
// The command shape is:
//
//	claude -p --output-format text --dangerously-skip-permissions [--model M]
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
	return runCLI(ctx, claudeBinary, buildClaudeArgs(rc.model), prompt, workDir, rc)
}

// buildClaudeArgs builds the argv (sans binary name) for a `claude` call.
// An empty model omits the --model flag and lets the CLI pick its default.
func buildClaudeArgs(model string) []string {
	args := []string{
		"-p",
		"--output-format", "text",
		"--dangerously-skip-permissions",
	}
	if strings.TrimSpace(model) != "" {
		args = append(args, "--model", strings.TrimSpace(model))
	}
	return args
}
