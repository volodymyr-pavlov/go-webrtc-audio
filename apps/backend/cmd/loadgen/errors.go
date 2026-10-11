package main

import "errors"

// errBadFlag is returned by parseFlags for a value that no run could use.
var errBadFlag = errors.New("loadgen: bad flag")

var errNoTrack = errors.New("the connection came up but no audio arrived")

var errServer = errors.New("server refused the listener")
