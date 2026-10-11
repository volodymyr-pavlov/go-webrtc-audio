package transport

import (
	"net"
	"net/netip"
	"testing"
)

func newAddrTable(max int) *BatchConn {
	return &BatchConn{
		addrs:    map[netip.AddrPort]*net.UDPAddr{},
		maxAddrs: max,
	}
}

func addrN(n int) netip.AddrPort {
	return netip.AddrPortFrom(netip.AddrFrom4([4]byte{10, 0, byte(n >> 8), byte(n)}), 4000)
}

func TestAddressTableKeepsEveryAddressWhileItFits(t *testing.T) {
	c := newAddrTable(5000)
	first := make([]*net.UDPAddr, 5000)
	for i := range first {
		first[i] = c.udpAddr(addrN(i))
	}
	// A second round, as the next tick would be, finds every address and allocates nothing.
	for i := range first {
		if c.udpAddr(addrN(i)) != first[i] {
			t.Fatalf("address %d was made again", i)
		}
	}
	if allocs := testing.AllocsPerRun(10, func() {
		for i := range first {
			c.udpAddr(addrN(i))
		}
	}); allocs != 0 {
		t.Fatalf("%v allocations for a round of known addresses, want none", allocs)
	}
}

func TestAddressTableDropsOneAddressNotAllWhenFull(t *testing.T) {
	c := newAddrTable(100)
	for i := 0; i < 100; i++ {
		c.udpAddr(addrN(i))
	}
	c.udpAddr(addrN(1000))
	if len(c.addrs) != 100 {
		t.Fatalf("%d addresses kept, want 100: one is dropped to make room, not all", len(c.addrs))
	}
}
