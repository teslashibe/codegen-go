package codegen

import (
	"os"
	"strings"
	"testing"
)

func TestCodexReasoningEffort(t *testing.T) {
	args := strings.Join(buildCodexArgs(resolveRunConfig(Config{Model: "gpt-6-astra", ReasoningEffort: "medium"}, nil), false), " ")
	if !strings.Contains(args, `model_reasoning_effort="medium"`) {
		t.Fatal(args)
	}
}
func TestInvocationEnvironmentIsolation(t *testing.T) {
	t.Setenv("CODEGEN_TEST_ACCOUNT", "parent")
	input := map[string]string{"CODEGEN_TEST_ACCOUNT": "selected"}
	option := WithEnvironment(input)
	input["CODEGEN_TEST_ACCOUNT"] = "modified"
	env := buildRunEnv(resolveRunConfig(Config{}, []RunOption{WithUnsetEnv("CODEGEN_TEST_ACCOUNT"), option}))
	count := 0
	for _, v := range env {
		if strings.HasPrefix(v, "CODEGEN_TEST_ACCOUNT=") {
			count++
			if v != "CODEGEN_TEST_ACCOUNT=selected" {
				t.Fatal(v)
			}
		}
	}
	if count != 1 || os.Getenv("CODEGEN_TEST_ACCOUNT") != "parent" {
		t.Fatal("environment leaked or duplicated")
	}
}
