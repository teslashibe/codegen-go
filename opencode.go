package codegen

import (
	"context"
	"strings"
)

// OpenCode runs SST's `opencode run` CLI non-interactively.
//
// The prompt is piped on stdin. Read-only behavior is represented by omitting
// --dangerously-skip-permissions and should be paired with a no-edits prompt by
// callers that need a read-only stage. Claude-only Config fields are ignored.
type OpenCode struct {
	cfg Config
}

// opencodeBinary is the executable name resolved on PATH. Var for tests.
var opencodeBinary = "opencode"

// NewOpenCode constructs an OpenCode agent. It does not probe PATH; if the
// binary is missing, the first Run call will surface the exec error.
func NewOpenCode(cfg Config) *OpenCode {
	return &OpenCode{cfg: cfg}
}

// Name implements Agent.
func (o *OpenCode) Name() string { return "opencode" }

// Run implements Agent.
func (o *OpenCode) Run(ctx context.Context, prompt, workDir string, opts ...RunOption) (Result, error) {
	rc := resolveRunConfig(o.cfg, opts)
	return runCLI(ctx, opencodeBinary, buildOpenCodeArgs(rc, false), prompt, workDir, rc)
}

// buildOpenCodeArgs assembles argv for `opencode run`.
// Streaming/JSON mode uses --format json; OpenCode events are surfaced through
// StreamEvent.Raw without schema normalization.
func buildOpenCodeArgs(rc runConfig, streaming bool) []string {
	args := []string{"run"}

	if streaming || strings.TrimSpace(rc.outputFormat) == "json" {
		args = append(args, "--format", "json")
	}
	if model := strings.TrimSpace(rc.model); model != "" {
		args = append(args, "-m", model)
	}
	if variant := strings.TrimSpace(rc.variant); variant != "" {
		args = append(args, "--variant", variant)
	}
	if rc.skipPermissions != nil && *rc.skipPermissions {
		args = append(args, "--dangerously-skip-permissions")
	}
	return args
}
