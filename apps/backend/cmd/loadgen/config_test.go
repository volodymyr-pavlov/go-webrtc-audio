package main

import (
	"errors"
	"testing"
)

func TestParseFlagsRefusesValuesNoRunCouldUse(t *testing.T) {
	for _, args := range [][]string{
		{"-concurrency", "0"},
		{"-concurrency", "-3"},
		{"-listeners", "0"},
		{"-listeners", "-1"},
		{"-leave", "1.5"},
		{"-leave", "-0.1"},
		{"-leave", "NaN"},
		{"-hold", "-1s"},
	} {
		if _, err := parseFlags(args); !errors.Is(err, errBadFlag) {
			t.Errorf("%v: got %v, want errBadFlag", args, err)
		}
	}
}

func TestParseFlagsAcceptsTheDefaultsAndTheEdges(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"-leave", "0"},
		{"-leave", "1"},
		{"-listeners", "1", "-concurrency", "1"},
	} {
		if _, err := parseFlags(args); err != nil {
			t.Errorf("%v: %v", args, err)
		}
	}
}
