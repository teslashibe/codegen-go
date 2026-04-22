package codegen

import (
	"context"
	"fmt"
	"strings"
)

// GenericCLI shells out to an arbitrary command, piping the prompt on stdin
// and capturing combined output. It exists so users can plug in Codex CLI,
// Aider, OpenHands, or any other agent that follows the "read a prompt from
// stdin, edit files in cwd" convention without us writing a bespoke wrapper.
type GenericCLI struct {
	cfg     Config
	command string
	args    []string
}

// NewGenericCLI constructs a GenericCLI agent from cfg.Command / cfg.Args.
func NewGenericCLI(cfg Config) *GenericCLI {
	return &GenericCLI{
		cfg:     cfg,
		command: strings.TrimSpace(cfg.Command),
		args:    append([]string(nil), cfg.Args...),
	}
}

// Name implements Agent.
func (g *GenericCLI) Name() string { return "generic" }

// Run implements Agent.
func (g *GenericCLI) Run(ctx context.Context, prompt, workDir string, opts ...RunOption) (Result, error) {
	if g.command == "" {
		return Result{ExitCode: -1}, fmt.Errorf("codegen: generic agent has no command configured (set Config.Command)")
	}
	rc := resolveRunConfig(g.cfg, opts)
	return runCLI(ctx, g.command, g.args, prompt, workDir, rc)
}
