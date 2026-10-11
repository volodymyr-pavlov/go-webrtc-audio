// Command loadgen connects many WebRTC listeners to the server and measures what that costs the server.
//
// It ramps up to -listeners connections, holds them for -hold, disconnects -leave of them, and waits for the server to
// settle. It reads the CPU time, memory and goroutines of the server from its /api/metrics address once a second, and from
// each listener it records how long the stream took to start, how many packets were lost, and the longest silence.
// The result is printed as a table row and, with -json, written as JSON.
package main
