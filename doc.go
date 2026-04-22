// Package codegen drives coding-agent CLIs (Claude Code, OpenAI Codex, Aider,
// OpenHands, Cline, and any other "prompt-on-stdin, edit-files-in-cwd" tool)
// from Go.
//
// The package exposes a small [Agent] interface so application code can treat
// every supported CLI uniformly. Two concrete implementations ship in the box:
//
//   - [ClaudeCode] (constructor [NewClaudeCode]) — wraps Anthropic's `claude`
//     binary in non-interactive print mode.
//   - [GenericCLI] (constructor [NewGenericCLI]) — shells out to any binary
//     supplied via [Config].Command / [Config].Args.
//
// Use [NewAgent] when the choice between Claude and a generic CLI is data-
// driven (e.g. controlled by an env var or per-user setting), and the direct
// constructors when the agent is fixed at compile time.
//
// [RunJSON] is a small helper for using an agent as a structured reasoner: it
// runs the agent and decodes the first well-formed JSON object or array in its
// output, tolerating markdown code fences and surrounding prose.
//
// Zero production dependencies — stdlib only.
package codegen
