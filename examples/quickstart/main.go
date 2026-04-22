// quickstart: run the Claude Code CLI on a working directory with a prompt
// and print whatever the agent emitted.
//
// Usage:
//
//	go run ./examples/quickstart "Add a doc comment to every exported func." /path/to/repo
//
// Prerequisites: `claude` on PATH and `claude login` already done.
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

	agent := codegen.NewClaudeCode(codegen.Config{})

	res, err := agent.Run(context.Background(), prompt, workDir)
	if err != nil {
		log.Fatalf("%s failed (exit=%d): %v\n--- output (tail) ---\n%s",
			agent.Name(), res.ExitCode, err, tail(res.Output, 4000))
	}

	fmt.Printf("--- %s output (%s) ---\n%s\n",
		agent.Name(), res.Duration.Round(1e6), res.Output)
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
