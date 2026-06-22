package codegen

import (
	"context"
	"strings"
)

// OpenCode runs the `opencode run` CLI non-interactively.
//
// The base command shape is:
//
//	opencode run [--format json] [--dangerously-skip-permissions] [-m provider/model] [--variant V]
//
// Model uses OpenCode's "provider/model" format (e.g. "anthropic/claude-...",
// "openai/gpt-5...", "google/gemini-..."). It is passed through verbatim via
// -m; an empty model lets OpenCode use its configured default.
//
// Prompt delivery: the prompt is piped on stdin. `opencode run` documents the
// message as positional args, but it also reads stdin when no message arg is
// given. Verified on OpenCode 1.17.9:
//
//	printf 'reply with the single word OK' | opencode run --format json \
//	    --dangerously-skip-permissions
//
// returned a model reply ("OK"), so we prefer stdin — it avoids the kernel's
// ARG_MAX limit on large prompts and matches how ClaudeCode/Codex deliver the
// prompt via runCLI.
//
// --format json emits NDJSON events for streaming. OpenCode's event schema
// differs from Claude's stream-json, so Stream() exposes each line verbatim via
// StreamEvent.Raw without normalizing it.
//
// runCLI already sets cmd.Dir = workDir, so OpenCode's --dir flag is
// unnecessary. The claude-only Config fields (AppendSystemPrompt, MCPConfigPath,
// AllowedTools, DisallowedTools), Sandbox, and ApprovalMode have no OpenCode
// equivalent and are silently ignored, matching how the package already treats
// cross-preset fields.
type OpenCode struct {
	cfg Config
}

// opencodeBinary is the executable name resolved on PATH. Var for tests.
var opencodeBinary = "opencode"

// NewOpenCode constructs an OpenCode agent. It does not probe PATH; if the
// binary is missing, the first Run call will surface the exec error.
func NewOpenCode(cfg Config) *OpenCode { return &OpenCode{cfg: cfg} }

// Name implements Agent.
func (o *OpenCode) Name() string { return "opencode" }

// Run implements Agent. The prompt is piped on stdin (verified to work for
// `opencode run` when no positional message arg is given).
func (o *OpenCode) Run(ctx context.Context, prompt, workDir string, opts ...RunOption) (Result, error) {
	rc := resolveRunConfig(o.cfg, opts)
	return runCLI(ctx, opencodeBinary, buildOpenCodeArgs(rc, false), prompt, workDir, rc)
}

// buildOpenCodeArgs assembles the argv (sans binary name) for an `opencode run`
// call from the resolved runConfig. The streaming flag forces `--format json`
// (NDJSON events) regardless of rc.outputFormat, since Stream() needs
// structured events on stdout.
func buildOpenCodeArgs(rc runConfig, streaming bool) []string {
	args := []string{"run"}

	if streaming || strings.TrimSpace(rc.outputFormat) == "json" {
		args = append(args, "--format", "json")
	}
	if rc.skipPermissions {
		args = append(args, "--dangerously-skip-permissions")
	}
	if m := strings.TrimSpace(rc.model); m != "" {
		// provider/model format, passed through verbatim.
		args = append(args, "-m", m)
	}
	if v := strings.TrimSpace(rc.variant); v != "" {
		args = append(args, "--variant", v)
	}
	return args
}
