package ai

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestExecFeedsStdinAndCollectsStdout(t *testing.T) {
	var out bytes.Buffer
	if err := Exec(context.Background(), strings.NewReader("hello there"), &out, nil, "cat"); err != nil {
		t.Fatal(err)
	}
	if out.String() != "hello there" {
		t.Fatalf("got %q", out.String())
	}
}

func TestExecPassesEveryArgumentAsItIsAndNeverThroughAShell(t *testing.T) {
	var out bytes.Buffer
	args := []string{"-n", "a; echo pwned", "$(whoami)", "`id`"}
	if err := Exec(context.Background(), nil, &out, nil, "echo", args...); err != nil {
		t.Fatal(err)
	}
	if out.String() != "a; echo pwned $(whoami) `id`" {
		t.Fatalf("got %q", out.String())
	}
}

func TestExecCollectsStderrAndReportsItsLastLineWhenTheProgramFails(t *testing.T) {
	var errs bytes.Buffer
	err := Exec(context.Background(), nil, nil, &errs, "sh", "-c", "echo first >&2; echo the real reason >&2; exit 3")
	if err == nil || !strings.Contains(err.Error(), "the real reason") || strings.Contains(err.Error(), "first") {
		t.Fatalf("got %v, want the last line of stderr and no more", err)
	}
	if !strings.Contains(errs.String(), "first") {
		t.Fatalf("the caller's stderr lost a line: %q", errs.String())
	}
}

func TestExecAcceptsNilStreams(t *testing.T) {
	if err := Exec(context.Background(), nil, nil, nil, "true"); err != nil {
		t.Fatal(err)
	}
}

func TestExecRefusesAnEmptyName(t *testing.T) {
	if err := Exec(context.Background(), nil, nil, nil, ""); !errors.Is(err, errNoProgram) {
		t.Fatalf("got %v, want errNoProgram", err)
	}
}

func TestExecStopsAProgramWhenItsContextEnds(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	if err := Exec(ctx, nil, nil, nil, "sleep", "30"); err == nil {
		t.Fatal("a program that ran past its deadline reported success")
	}
	if time.Since(started) > 10*time.Second {
		t.Fatalf("it took %v", time.Since(started))
	}
}

func TestLastLineKeepsOnlyTheEndOfALongOutput(t *testing.T) {
	var l lastLine
	for i := 0; i < 1000; i++ {
		_, _ = l.Write([]byte("noise noise noise\n"))
	}
	_, _ = l.Write([]byte("the message\n"))
	if got := l.String(); got != "the message" || len(l.tail) > 512 {
		t.Fatalf("got %q holding %d bytes", got, len(l.tail))
	}
}
