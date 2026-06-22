package codegen

import (
	"context"
	"strings"
)

// Gemini runs Google's `gemini` CLI in non-interactive headless mode.
//
// Headless mode requires -p; without it the CLI starts an interactive TUI.
// The prompt is passed as the -p value rather than stdin so Run and Stream use
// the same verified invocation shape. Claude-only Config fields are ignored.
type Gemini struct {
	cfg Config
}

// geminiBinary is the executable name resolved on PATH. Var for tests.
var geminiBinary = "gemini"

// NewGemini constructs a Gemini agent. It does not probe PATH; if the binary is
// missing, the first Run call will surface the exec error.
func NewGemini(cfg Config) *Gemini {
	return &Gemini{cfg: cfg}
}

// Name implements Agent.
func (g *Gemini) Name() string { return "gemini" }

// Run implements Agent.
func (g *Gemini) Run(ctx context.Context, prompt, workDir string, opts ...RunOption) (Result, error) {
	rc := resolveRunConfig(g.cfg, opts)
	return runCLI(ctx, geminiBinary, buildGeminiArgs(rc, false, prompt), "", workDir, rc)
}

// buildGeminiArgs assembles argv for `gemini`.
// Streaming forces stream-json. The prompt is always delivered through -p.
func buildGeminiArgs(rc runConfig, streaming bool, prompt string) []string {
	args := []string{}

	switch {
	case streaming:
		args = append(args, "-o", "stream-json")
	case strings.TrimSpace(rc.outputFormat) != "":
		args = append(args, "-o", strings.TrimSpace(rc.outputFormat))
	default:
		args = append(args, "-o", "text")
	}

	mode := strings.TrimSpace(rc.approvalMode)
	if mode == "" {
		mode = "yolo"
	}
	args = append(args, "--approval-mode", mode, "--skip-trust")

	if model := strings.TrimSpace(rc.model); model != "" {
		args = append(args, "-m", model)
	}

	return append(args, "-p", prompt)
}
