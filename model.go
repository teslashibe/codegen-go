package codegen

import "time"

// Result is the outcome of a single coding-agent invocation.
type Result struct {
	// Output is the combined stdout+stderr captured from the agent process,
	// possibly truncated to the configured MaxOutputBytes.
	Output string
	// ExitCode is the process exit code. 0 on success, the OS-reported exit
	// code on a non-zero process exit, or -1 when the process failed to start
	// or was terminated by context cancellation before exiting cleanly.
	ExitCode int
	// Duration is the wall-clock time the agent ran for.
	Duration time.Duration
	// Truncated is true when the agent produced more bytes than the configured
	// MaxOutputBytes and the surplus was discarded.
	Truncated bool
}

// cappedBuffer is an io.Writer that captures up to max bytes and silently
// drops the rest, flipping Truncated once the cap is hit. A non-positive
// max disables the cap.
type cappedBuffer struct {
	buf       []byte
	max       int
	truncated bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if b.max <= 0 {
		b.buf = append(b.buf, p...)
		return n, nil
	}
	remaining := b.max - len(b.buf)
	if remaining <= 0 {
		b.truncated = true
		return n, nil
	}
	if len(p) > remaining {
		b.buf = append(b.buf, p[:remaining]...)
		b.truncated = true
		return n, nil
	}
	b.buf = append(b.buf, p...)
	return n, nil
}

func (b *cappedBuffer) String() string { return string(b.buf) }
