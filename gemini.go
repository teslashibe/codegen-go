package codegen

import (
	"context"
	"strings"
)

// Gemini runs Google's `gemini` CLI in non-interactive (headless) mode.
//
// The base command shape is:
//
//	gemini -o <fmt> --approval-mode <mode> --skip-trust [-m MODEL] -p <prompt>
//
// Critical behaviours this preset bakes in:
//
//   - Headless mode REQUIRES -p. Without it the CLI launches an interactive
//     TUI and will hang in an automated pipeline. The preset ALWAYS passes -p.
//   - The prompt is delivered as the -p argv value (Gemini's documented
//     headless path), not on stdin. Empty `-p ”` + stdin is unreliable, so
//     Run uses runCLIArgvPrompt (no stdin) and bakes the prompt into argv.
//   - --skip-trust trusts the current workspace for this session so a fresh
//     worktree directory doesn't block on an interactive trust prompt.
//
// Verified incantation (Gemini 0.47.0), prompt in argv, nothing on stdin:
//
//	gemini -p 'reply with the single word OK' --approval-mode plan -o text --skip-trust
//
// --approval-mode plan is a genuine READ-ONLY mode (no edits); use
// WithApprovalMode("plan") for plan/review stages. The default is "yolo"
// (auto-approve all tools) for autonomous edits.
//
// The claude-only Config fields (AppendSystemPrompt, MCPConfigPath,
// AllowedTools, DisallowedTools) and Sandbox have no Gemini equivalent and are
// silently ignored, matching how the package already treats cross-preset
// fields.
type Gemini struct {
	cfg Config
}

// geminiBinary is the executable name resolved on PATH. Var for tests.
var geminiBinary = "gemini"

// NewGemini constructs a Gemini agent. It does not probe PATH; if the binary is
// missing, the first Run call will surface the exec error.
func NewGemini(cfg Config) *Gemini { return &Gemini{cfg: cfg} }

// Name implements Agent.
func (g *Gemini) Name() string { return "gemini" }

// Run implements Agent. The prompt is passed via -p in argv (no stdin), so
// headless mode is triggered reliably.
func (g *Gemini) Run(ctx context.Context, prompt, workDir string, opts ...RunOption) (Result, error) {
	rc := resolveRunConfig(g.cfg, opts)
	return runCLIArgvPrompt(ctx, geminiBinary, buildGeminiArgs(rc, false, prompt), workDir, rc)
}

// buildGeminiArgs assembles the argv (sans binary name) for a `gemini` call
// from the resolved runConfig. The prompt is appended as the -p value. The
// streaming flag forces `-o stream-json` regardless of rc.outputFormat, since
// Stream() needs structured events on stdout.
func buildGeminiArgs(rc runConfig, streaming bool, prompt string) []string {
	args := []string{}

	// Output format. Streaming forces stream-json.
	switch {
	case streaming:
		args = append(args, "-o", "stream-json")
	case strings.TrimSpace(rc.outputFormat) != "":
		args = append(args, "-o", rc.outputFormat)
	default:
		args = append(args, "-o", "text")
	}

	// Approval / read-only mode. Empty defaults to "yolo" (autonomous edits).
	mode := strings.TrimSpace(rc.approvalMode)
	if mode == "" {
		mode = "yolo"
	}
	args = append(args, "--approval-mode", mode)

	// Trust the working dir so a fresh worktree doesn't block on a prompt.
	args = append(args, "--skip-trust")

	if m := strings.TrimSpace(rc.model); m != "" {
		args = append(args, "-m", m)
	}

	// -p triggers headless mode; the prompt is its value (passed via argv).
	args = append(args, "-p", prompt)
	return args
}
