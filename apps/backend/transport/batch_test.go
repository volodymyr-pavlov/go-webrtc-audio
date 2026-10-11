package transport

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/metrics"
)

func listen(t *testing.T) *net.UDPConn {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	return conn
}

// startReceive reads packets from conn in the background, from now on, until count have come or the deadline passes. The
// function it returns waits for that and returns the first byte pair of each packet as a number. The counter holds the
// number of packets read so far, so a sender can wait for the receiver instead of overrunning its socket buffer, which
// a Linux limit caps.
func startReceive(conn *net.UDPConn, count int) (func(t *testing.T) map[int]bool, *atomic.Int64) {
	received := new(atomic.Int64)
	type result struct {
		got map[int]bool
		err error
	}
	doneCh := make(chan result, 1)
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	go func() {
		got := map[int]bool{}
		buf := make([]byte, 2048)
		for len(got) < count {
			n, _, err := conn.ReadFromUDPAddrPort(buf)
			if err != nil {
				doneCh <- result{got, fmt.Errorf("got %d of %d packets: %w", len(got), count, err)}
				return
			}
			if n != 100 {
				doneCh <- result{got, fmt.Errorf("packet of %d bytes, want 100", n)}
				return
			}
			got[int(buf[0])<<8|int(buf[1])] = true
			received.Add(1)
		}
		doneCh <- result{got: got}
	}()
	return func(t *testing.T) map[int]bool {
		t.Helper()
		r := <-doneCh
		if r.err != nil {
			t.Fatal(r.err)
		}
		return r.got
	}, received
}

// catchUp waits until the receiver behind received has read count packets, or gives up after a while and lets the
// receiver report what is missing.
func catchUp(received *atomic.Int64, count int) {
	deadline := time.Now().Add(5 * time.Second)
	for received.Load() < int64(count) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
}

func TestEveryQueuedPacketArrivesIntact(t *testing.T) {
	receiver := listen(t)
	defer receiver.Close()
	counters := metrics.New()
	sender := NewBatchConn(listen(t), BatchOptions{
		Size:    16,
		Metrics: counters,
	})
	defer sender.Close()
	to := receiver.LocalAddr().(*net.UDPAddr).AddrPort()

	const count = 1000
	wait, received := startReceive(receiver, count)
	// Send in rounds and let the receiver catch up after each, so the test checks the sender and not how many packets
	// the kernel buffers when the machine is slow.
	const round, writers = 50, 4
	for start := 0; start < count; start += round {
		var wg sync.WaitGroup
		for w := range writers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := start + w; i < start+round; i += writers {
					p := make([]byte, 100)
					p[0], p[1] = byte(i>>8), byte(i)
					if n, err := sender.WriteToAddrPort(p, to); err != nil || n != 100 {
						t.Errorf("write %d: %d, %v", i, n, err)
					}
				}
			}()
		}
		wg.Wait()
		catchUp(received, start+round)
	}
	got := wait(t)
	for i := range count {
		if !got[i] {
			t.Fatalf("packet %d never arrived", i)
		}
	}
	if counters.UDPPackets.Load() != count {
		t.Fatalf("UDPPackets = %d, want %d", counters.UDPPackets.Load(), count)
	}
}

func TestPacketsAreBatchedOnLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("only Linux sends a batch with one system call")
	}
	receiver := listen(t)
	defer receiver.Close()
	counters := metrics.New()
	sender := NewBatchConn(listen(t), BatchOptions{
		Size:    64,
		Delay:   time.Millisecond,
		Metrics: counters,
	})
	defer sender.Close()
	to := receiver.LocalAddr().(*net.UDPAddr).AddrPort()
	const count, round = 500, 100
	wait, received := startReceive(receiver, count)

	// Send in rounds and let the receiver catch up after each. The default socket buffer of Linux holds about 240
	// packets of this size, and on one CPU the receiver reads none of them before the sender is done, so sending all at
	// once loses the rest. A round still fills more than one batch.
	for start := 0; start < count; start += round {
		for i := start; i < start+round; i++ {
			p := make([]byte, 100)
			p[0], p[1] = byte(i>>8), byte(i)
			if _, err := sender.WriteToAddrPort(p, to); err != nil {
				t.Fatal(err)
			}
		}
		catchUp(received, start+round)
	}
	wait(t)
	if sends, packets := counters.UDPSends.Load(), counters.UDPPackets.Load(); sends*4 > packets {
		t.Fatalf("%d packets took %d system calls: they are not batched", packets, sends)
	}
}

func TestCloseSendsWhatIsQueued(t *testing.T) {
	receiver := listen(t)
	defer receiver.Close()
	sender := NewBatchConn(listen(t), BatchOptions{Delay: time.Second})
	to := receiver.LocalAddr().(*net.UDPAddr).AddrPort()
	wait, _ := startReceive(receiver, 10)
	for i := range 10 {
		p := make([]byte, 100)
		p[1] = byte(i)
		if _, err := sender.WriteToAddrPort(p, to); err != nil {
			t.Fatal(err)
		}
	}
	if err := sender.Close(); err != nil {
		t.Fatal(err)
	}
	wait(t)
	if err := sender.Close(); err != nil {
		t.Fatalf("second Close returned %v", err)
	}
}

func TestWriteAfterCloseAndToABadAddress(t *testing.T) {
	sender := NewBatchConn(listen(t), BatchOptions{})
	if _, err := sender.WriteTo([]byte{1}, &net.TCPAddr{}); !errors.Is(err, ErrBadAddr) {
		t.Fatalf("got %v, want ErrBadAddr", err)
	}
	_ = sender.Close()
	if _, err := sender.WriteToAddrPort([]byte{1}, netip.MustParseAddrPort("127.0.0.1:9")); !errors.Is(err, ErrClosed) {
		t.Fatalf("got %v, want ErrClosed", err)
	}
}

func TestAFullQueueDropsInsteadOfBlocking(t *testing.T) {
	receiver := listen(t)
	defer receiver.Close()
	counters := metrics.New()
	sender := NewBatchConn(listen(t), BatchOptions{
		MaxQueued: 4,
		Delay:     time.Second,
		Metrics:   counters,
	})
	defer sender.Close()
	to := receiver.LocalAddr().(*net.UDPAddr).AddrPort()

	// The sender takes the first packet at once and then waits a second for more to join it, so it holds one packet
	// and the queue holds four.
	if _, err := sender.WriteToAddrPort(make([]byte, 100), to); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	for range 9 {
		if _, err := sender.WriteToAddrPort(make([]byte, 100), to); err != nil {
			t.Fatal(err)
		}
	}
	if got := counters.Dropped.Load(); got != 5 {
		t.Fatalf("Dropped = %d, want 5", got)
	}
}
