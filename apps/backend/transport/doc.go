// Package transport holds the UDP socket that the server sends its media through.
//
// A [BatchConn] sits between Pion's ICE multiplexer and a real UDP socket. A write returns as soon as the packet is on
// a queue, and one goroutine sends the queued packets together with a single system call. On Linux that call is
// sendmmsg, which sends a whole batch of packets for the price of one entry into the kernel. A server that sends a
// packet to every listener every few milliseconds spends a large part of its CPU on those entries, so sending them in
// batches saves some of it.
//
// Outside Linux the batch is still collected and sent, one packet at a time, so the server behaves the same but gains
// nothing.
package transport
