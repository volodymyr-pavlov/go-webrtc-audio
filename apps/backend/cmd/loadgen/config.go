package main

import (
	"flag"
	"fmt"
	"time"
)

// configFlag is a bit of [config.flags].
type configFlag uint8

const (
	flagAllInterfaces configFlag = 1 << iota
)

// config is what the command line sets.
type config struct {
	server      string
	listeners   int
	hold        time.Duration
	idle        time.Duration
	settle      time.Duration
	leave       float64
	concurrency int
	label       string
	jsonPath    string
	flags       configFlag
}

func parseFlags(args []string) (config, error) {
	var c config
	set := flag.NewFlagSet("loadgen", flag.ContinueOnError)
	set.StringVar(&c.server, "server", "http://127.0.0.1:8080", "base address of the server")
	set.IntVar(&c.listeners, "listeners", 100, "number of listeners")
	set.DurationVar(&c.hold, "hold", 30*time.Second, "how long all listeners stay connected")
	set.DurationVar(&c.idle, "idle", 5*time.Second, "how long to measure the server before any listener connects")
	set.DurationVar(&c.settle, "settle", 10*time.Second, "how long to watch the server after listeners leave")
	set.Float64Var(&c.leave, "leave", 1, "fraction of the listeners that disconnect after the hold, 0 to 1")
	set.IntVar(&c.concurrency, "concurrency", 32, "connections set up at the same time")
	set.StringVar(&c.label, "label", "", "name of this run in the report, such as the mode of the server")
	set.StringVar(&c.jsonPath, "json", "", "write the report as JSON to this file")
	allInterfaces := set.Bool("all-interfaces", false, "gather candidates on every interface, not only the loopback")
	if err := set.Parse(args); err != nil {
		return config{}, err
	}
	if *allInterfaces {
		c.flags |= flagAllInterfaces
	}
	switch {
	case c.listeners < 1:
		return config{}, fmt.Errorf("%w: -listeners must be at least 1, not %d", errBadFlag, c.listeners)
	case c.concurrency < 1:
		return config{}, fmt.Errorf("%w: -concurrency must be at least 1, not %d", errBadFlag, c.concurrency)
	case !(c.leave >= 0 && c.leave <= 1):
		return config{}, fmt.Errorf("%w: -leave must be from 0 to 1, not %v", errBadFlag, c.leave)
	case c.hold < 0 || c.idle < 0 || c.settle < 0:
		return config{}, fmt.Errorf("%w: -hold, -idle and -settle must not be negative", errBadFlag)
	}
	return c, nil
}
