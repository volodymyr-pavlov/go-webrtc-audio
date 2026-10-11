// Package fanout copies each RTP packet of a stream to every listener, in a way that costs the least when the number
// of listeners is large.
//
// A [Router] spreads its listeners over a fixed number of shards, by default one per CPU core. A packet written to the
// router is handed to each shard once, and the shard puts it on a bounded queue of each of its listeners without
// waiting. A writer goroutine of the listener takes it from the queue and writes it to the network. A listener that
// cannot keep up loses packets from its own queue and never delays the others. A listener that loses too many packets
// in a row is evicted.
//
// A negative [RouterOptions.QueueLen] removes the queues and the writer goroutines: the shard writes to each listener
// itself. Over a plain socket that was measured to cost more CPU, so the router keeps its queues; over the batching
// socket of the engine, where a write only joins a shared queue, it cost less, and the engine asks for it.
//
// The packet is created once, by the playout, and every listener shares it: the payload is never copied here.
package fanout
