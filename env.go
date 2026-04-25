package codegen

import (
	"runtime"
	"strings"
)

// envKeyEqual compares two environment-variable names using the
// platform's canonical case sensitivity. Unix kernels treat env
// names as case-sensitive bytes; Windows treats them as
// case-insensitive (CMD and PowerShell upper-case keys, and
// %PATH% / %Path% / %path% all resolve identically). Splitting
// this into a tiny helper keeps the env-filter walk in
// buildChildEnv readable.
func envKeyEqual(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}
