package ai

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

// runTimeout is how long a program may run when the context has no deadline of its own.
const runTimeout = 10 * time.Minute

// A Runner starts a program with its standard streams connected to the given reader and writers, as [os/exec.Cmd] does,
// and waits for it. They are the standard [io.Reader] and [io.Writer], so a caller can feed a program a string, collect
// its output in a buffer, stream it to a file, or discard it, without a type of its own for any of those.
//
// A Runner starts the program with the arguments. Its standard input is stdin, its standard output is stdout and its
// standard error is stderr; any of them may be nil, which connects the stream to nothing. A program that exits with an
// error returns that error, with the last line of its standard error, so that its own message can be shown.
type Runner func(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, name string, args ...string) error

var _ Runner = Exec

// Exec is the [Runner] that starts real programs. It returns [errNoProgram] for an empty name.
func Exec(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
	if name == "" {
		return errNoProgram
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, runTimeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = 5 * time.Second
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	last := &lastLine{}
	if stderr != nil {
		cmd.Stderr = io.MultiWriter(stderr, last)
	} else {
		cmd.Stderr = last
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("run %s: %w: %s", name, err, last.String())
	}
	return nil
}

// lastLine is an [io.Writer] that keeps the end of what is written to it, up to 512 bytes, which is where a program
// says what went wrong, without holding all of its output.
type lastLine struct {
	tail []byte
}

var _ io.Writer = &lastLine{}

// Write implements [io.Writer]. It never fails.
func (l *lastLine) Write(p []byte) (int, error) {
	l.tail = append(l.tail, p...)
	if len(l.tail) > 512 {
		l.tail = l.tail[len(l.tail)-512:]
	}
	return len(p), nil
}

// String returns the last line of what was written.
func (l *lastLine) String() string {
	return tail(string(l.tail))
}

// tail returns the last line of a program's message, which is the one that says what went wrong.
func tail(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	return s
}
