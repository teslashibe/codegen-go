package codegen

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Agent runs a coding-agent CLI in a working directory with a prompt.
type Agent interface {
	// Name returns the agent identifier (e.g. "claude-code", "generic").
	Name() string

	// Run executes the agent with the given prompt in workDir. The agent is
	// expected to make any code changes in workDir. Returns the captured
	// output and any error from the underlying process.
	Run(ctx context.Context, prompt string, workDir string, opts ...RunOption) (Result, error)
}

// Config configures both the factory and individual Agent implementations.
// Fields are interpreted by whichever implementation NewAgent selects.
type Config struct {
	// Type selects the implementation: "claude-code" (default) or "generic".
	Type string
	// Model is an optional model override (passed to claude --model, ignored
	// by the generic CLI).
	Model string
	// Timeout caps a single Run invocation. Zero falls back to DefaultTimeout.
	Timeout time.Duration
	// MaxOutputBytes caps captured combined output. Zero falls back to
	// DefaultMaxOutputBytes; negative disables the cap.
	MaxOutputBytes int
	// Command is the binary the GenericCLI agent invokes (ignored by
	// claude-code).
	Command string
	// Args are extra arguments prepended to the GenericCLI argv (ignored by
	// claude-code).
	Args []string

	// AppendSystemPrompt is appended to claude-code's built-in system prompt
	// via --append-system-prompt. Useful for injecting brand persona, tool
	// usage instructions, or domain rules without replacing the default
	// behaviour. Ignored by the generic CLI.
	AppendSystemPrompt string
	// MCPConfigPath points at a Claude Code MCP config file (JSON) loaded
	// via --mcp-config. Lets callers expose custom MCP servers (HTTP, SSE,
	// stdio) to the agent. Ignored by the generic CLI.
	MCPConfigPath string
	// AllowedTools is a list of tool patterns passed to --allowedTools (e.g.
	// "Read", "Bash(git:*)", "mcp__polybot__trader_place_order"). An empty
	// slice omits the flag and lets Claude Code apply its defaults. Ignored
	// by the generic CLI.
	AllowedTools []string
	// DisallowedTools is a list of tool patterns passed to --disallowedTools.
	// Used to block specific tools while inheriting defaults. Ignored by the
	// generic CLI.
	DisallowedTools []string
	// OutputFormat overrides claude-code's --output-format. Empty defaults to
	// "text". Stream() forces "stream-json" regardless of this value. Ignored
	// by the generic CLI.
	OutputFormat string
}

// Sensible defaults applied when Config / RunOption leave a field unset.
const (
	DefaultTimeout        = 30 * time.Minute
	DefaultMaxOutputBytes = 10 * 1024 * 1024 // 10 MiB
)

// RunOption mutates the per-invocation runConfig.
type RunOption func(*runConfig)

type runConfig struct {
	model              string
	timeout            time.Duration
	maxOutputBytes     int
	appendSystemPrompt string
	mcpConfigPath      string
	allowedTools       []string
	disallowedTools    []string
	outputFormat       string
}

// WithModel overrides the model passed to the underlying CLI for this Run.
// Ignored by agents that do not understand a model flag.
func WithModel(model string) RunOption {
	return func(c *runConfig) { c.model = model }
}

// WithTimeout overrides the per-Run timeout. Pass a non-positive value to
// disable the timeout entirely (the parent ctx still applies).
func WithTimeout(d time.Duration) RunOption {
	return func(c *runConfig) { c.timeout = d }
}

// WithMaxOutputBytes caps captured combined output. Pass a negative value to
// disable the cap.
func WithMaxOutputBytes(n int) RunOption {
	return func(c *runConfig) { c.maxOutputBytes = n }
}

// WithAppendSystemPrompt overrides Config.AppendSystemPrompt for this Run.
// Pass an empty string to clear the value and fall back to claude-code's
// defaults. Ignored by agents that do not support a system prompt flag.
func WithAppendSystemPrompt(s string) RunOption {
	return func(c *runConfig) { c.appendSystemPrompt = s }
}

// WithMCPConfig overrides Config.MCPConfigPath for this Run. Pass an empty
// string to disable MCP loading. Ignored by agents that do not understand
// MCP configuration.
func WithMCPConfig(path string) RunOption {
	return func(c *runConfig) { c.mcpConfigPath = path }
}

// WithAllowedTools overrides Config.AllowedTools for this Run. Passing zero
// arguments clears the flag (claude-code falls back to its defaults).
func WithAllowedTools(tools ...string) RunOption {
	return func(c *runConfig) {
		c.allowedTools = append([]string(nil), tools...)
	}
}

// WithDisallowedTools overrides Config.DisallowedTools for this Run. Passing
// zero arguments clears the flag.
func WithDisallowedTools(tools ...string) RunOption {
	return func(c *runConfig) {
		c.disallowedTools = append([]string(nil), tools...)
	}
}

// WithOutputFormat overrides Config.OutputFormat for this Run. Stream()
// always forces "stream-json" so this option is mainly useful for choosing
// "json" vs the default "text" in non-streaming Run calls.
func WithOutputFormat(format string) RunOption {
	return func(c *runConfig) { c.outputFormat = format }
}

// NewAgent constructs an Agent from the given Config.
func NewAgent(cfg Config) (Agent, error) {
	switch strings.TrimSpace(cfg.Type) {
	case "", "claude-code":
		return NewClaudeCode(cfg), nil
	case "generic":
		return NewGenericCLI(cfg), nil
	default:
		return nil, fmt.Errorf("codegen: unknown agent type %q", cfg.Type)
	}
}

// resolveRunConfig folds Config defaults and per-call options into a single
// runConfig used by runCLI. RunOption values take precedence over Config.
func resolveRunConfig(cfg Config, opts []RunOption) runConfig {
	rc := runConfig{
		model:              cfg.Model,
		timeout:            cfg.Timeout,
		maxOutputBytes:     cfg.MaxOutputBytes,
		appendSystemPrompt: cfg.AppendSystemPrompt,
		mcpConfigPath:      cfg.MCPConfigPath,
		allowedTools:       append([]string(nil), cfg.AllowedTools...),
		disallowedTools:    append([]string(nil), cfg.DisallowedTools...),
		outputFormat:       cfg.OutputFormat,
	}
	if rc.timeout == 0 {
		rc.timeout = DefaultTimeout
	}
	if rc.maxOutputBytes == 0 {
		rc.maxOutputBytes = DefaultMaxOutputBytes
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&rc)
		}
	}
	return rc
}

// runCLI executes the named binary with args, piping prompt to stdin and
// capturing combined output (capped at rc.maxOutputBytes). It honours
// rc.timeout via a derived context.
func runCLI(ctx context.Context, name string, args []string, prompt, workDir string, rc runConfig) (Result, error) {
	if rc.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, rc.timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = workDir
	cmd.Stdin = strings.NewReader(prompt)

	buf := &cappedBuffer{max: rc.maxOutputBytes}
	cmd.Stdout = buf
	cmd.Stderr = buf

	start := time.Now()
	err := cmd.Run()
	duration := time.Since(start)

	res := Result{
		Output:    buf.String(),
		Duration:  duration,
		Truncated: buf.truncated,
	}

	switch {
	case err == nil:
		res.ExitCode = 0
		return res, nil
	case cmd.ProcessState != nil:
		// Process started and exited non-zero (or signalled). Surface the
		// real exit code alongside the error so callers can branch on either.
		res.ExitCode = cmd.ProcessState.ExitCode()
	default:
		// Failed to start, or context cancelled before any state was set.
		res.ExitCode = -1
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return res, fmt.Errorf("%s: exit code %d: %w", name, res.ExitCode, err)
	}
	return res, fmt.Errorf("%s: %w", name, err)
}
