// with-codex: same surface as quickstart, but driving the OpenAI Codex CLI
// (or any other "prompt-on-stdin, edit-in-cwd" tool) via NewGenericCLI.
//
// Usage:
//
//	go run ./examples/with-codex "Refactor main.go to split networking into a sub-package." /path/to/repo
//
// Prerequisites: `codex` on PATH and authenticated. Swap Command/Args for
// Aider, OpenHands, Cline, or your own binary.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	codegen "github.com/teslashibe/codegen-go"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatalf("usage: %s <prompt> [workDir]", filepath.Base(os.Args[0]))
	}

	prompt := os.Args[1]
	workDir, _ := os.Getwd()
	if len(os.Args) >= 3 {
		workDir = os.Args[2]
	}

	agent := codegen.NewGenericCLI(codegen.Config{
		Command: "codex",
		Args:    []string{"--auto-approve"},
	})

	res, err := agent.Run(context.Background(), prompt, workDir)
	if err != nil {
		log.Fatalf("%s failed (exit=%d): %v\n--- output (tail) ---\n%s",
			agent.Name(), res.ExitCode, err, tail(res.Output, 4000))
	}

	fmt.Printf("--- %s (%s) ---\n%s\n",
		agent.Name(), res.Duration.Round(1e6), res.Output)
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
