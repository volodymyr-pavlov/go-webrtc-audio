package stream

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pion/rtp"

	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/inmem"
	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/playlist"
)

type collected struct {
	packet *rtp.Packet
	at     time.Time
}

type collector struct {
	entries inmem.List[collected]
}

// Write parses the bytes, which the playout reuses, into a packet of its own.
func (c *collector) Write(b []byte) (int, error) {
	packet := &rtp.Packet{}
	if err := packet.Unmarshal(bytes.Clone(b)); err != nil {
		return 0, err
	}
	c.entries.Add(collected{
		packet: packet,
		at:     time.Now(),
	})
	return len(b), nil
}

func (c *collector) len() int {
	return c.entries.Len()
}

// packets returns the packets collected so far.
func (c *collector) packets() []*rtp.Packet {
	entries := c.entries.All()
	out := make([]*rtp.Packet, len(entries))
	for i, e := range entries {
		out[i] = e.packet
	}
	return out
}

// times returns when each packet was collected.
func (c *collector) times() []time.Time {
	entries := c.entries.All()
	out := make([]time.Time, len(entries))
	for i, e := range entries {
		out[i] = e.at
	}
	return out
}

// waitForPackets blocks until the collector holds n packets or five seconds pass.
func waitForPackets(t *testing.T, c *collector, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for c.len() < n {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d packets, have %d", n, c.len())
		}
		time.Sleep(time.Millisecond)
	}
}

func tinyPlaylist() *playlist.Playlist {
	return &playlist.Playlist{
		Tracks: []playlist.Track{
			{
				Name: "a",
				Packets: [][]byte{
					{1},
					{2},
					{3},
				},
			},
		},
	}
}

func TestPlayoutSendsOnePacketEveryTwentyMilliseconds(t *testing.T) {
	sink := &collector{}
	playout, err := NewPlayout(PlayoutOptions{
		Playlist: tinyPlaylist(),
		Sink:     sink,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		_ = playout.Do(ctx)
	}()
	waitForPackets(t, sink, 10)
	cancel()
	<-doneCh

	got := sink.len()
	if got < 10 {
		t.Fatalf("sent %d packets, want at least 10", got)
	}
	for i, p := range sink.packets() {
		if p.SequenceNumber != uint16(i) {
			t.Fatalf("packet %d has sequence %d", i, p.SequenceNumber)
		}
		if p.Timestamp != uint32(i)*960 {
			t.Fatalf("packet %d has timestamp %d", i, p.Timestamp)
		}
		if p.Marker != (i == 0) {
			t.Fatalf("packet %d has marker %v", i, p.Marker)
		}
		if p.Payload[0] != byte(i%3+1) {
			t.Fatalf("packet %d has payload %v", i, p.Payload)
		}
	}
	times := sink.times()
	spread := times[len(times)-1].Sub(times[0])
	want := time.Duration(got-1) * 20 * time.Millisecond
	if spread < want-10*time.Millisecond || spread > want+10*time.Millisecond {
		t.Fatalf("%d packets spread over %v, want %v: the playout drifts", got, spread, want)
	}
}

func TestPlayoutRejectsMissingInput(t *testing.T) {
	if _, err := NewPlayout(PlayoutOptions{Sink: &collector{}}); !errors.Is(err, ErrNoPlaylist) {
		t.Errorf("got %v, want ErrNoPlaylist", err)
	}
	if _, err := NewPlayout(PlayoutOptions{Playlist: tinyPlaylist()}); !errors.Is(err, ErrNoSink) {
		t.Errorf("got %v, want ErrNoSink", err)
	}
}

func TestPlayoutFollowsTheFrameLengthOfThePlaylist(t *testing.T) {
	list := tinyPlaylist()
	list.PacketDuration = 60 * time.Millisecond
	sink := &collector{}
	playout, err := NewPlayout(PlayoutOptions{
		Playlist: list,
		Sink:     sink,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		_ = playout.Do(ctx)
	}()
	waitForPackets(t, sink, 4)
	cancel()
	<-doneCh

	got := sink.len()
	if got < 4 {
		t.Fatalf("sent %d packets of 60 ms frames, want at least 4", got)
	}
	for i, p := range sink.packets() {
		if p.Timestamp != uint32(i)*2880 {
			t.Fatalf("packet %d has timestamp %d, want %d", i, p.Timestamp, i*2880)
		}
	}
}

func TestPlayoutRejectsATrackWithNoPackets(t *testing.T) {
	list := tinyPlaylist()
	list.Tracks = append(list.Tracks, playlist.Track{Name: "empty"})
	if _, err := NewPlayout(PlayoutOptions{
		Playlist: list,
		Sink:     &collector{},
	}); !errors.Is(err, ErrNoPlaylist) {
		t.Errorf("got %v, want ErrNoPlaylist", err)
	}
}

// twoTracks has three packets in the first track and three in the second; the payloads are 1 to 6.
func twoTracks() *playlist.Playlist {
	return &playlist.Playlist{
		Tracks: []playlist.Track{
			{
				Name: "a",
				Packets: [][]byte{
					{1},
					{2},
					{3},
				},
			},
			{
				Name: "b",
				Packets: [][]byte{
					{4},
					{5},
					{6},
				},
			},
		},
	}
}

// payloads runs a playout until the sink holds n packets and returns their payloads in order.
func payloads(t *testing.T, opts PlayoutOptions, n int, before func()) []*rtp.Packet {
	t.Helper()
	sink := &collector{}
	opts.Sink = sink
	playout, err := NewPlayout(opts)
	if err != nil {
		t.Fatal(err)
	}
	if before != nil {
		before()
	}
	ctx, cancel := context.WithCancel(context.Background())
	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		_ = playout.Do(ctx)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for sink.len() < n {
		if time.Now().After(deadline) {
			t.Fatalf("only %d of %d packets", sink.len(), n)
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-doneCh
	return sink.packets()[:n]
}

func bytesOf(packets []*rtp.Packet) []byte {
	out := make([]byte, 0, len(packets))
	for _, p := range packets {
		out = append(out, p.Payload[0])
	}
	return out
}

func TestAClipPlaysInFrontOfTheTrackItIntroducesAndNowhereElse(t *testing.T) {
	interludes := &Interludes{}
	startedCh := make(chan int, 4)
	endedCh := make(chan int, 4)
	got := payloads(t, PlayoutOptions{
		Playlist:   twoTracks(),
		Interludes: interludes,
		OnClip: func(clip *Clip, event ClipEvent) {
			if event&ClipStarted != 0 {
				startedCh <- clip.Before
			} else {
				endedCh <- clip.Before
			}
		},
	}, 9, func() {
		interludes.Offer(&Clip{
			Before: 1,
			Packets: [][]byte{
				{90},
				{91},
			},
			Text: "Here is track b.",
		})
	})

	// Track a, then the clip, then track b, then track a again with no clip: a clip plays once.
	want := []byte{1, 2, 3, 90, 91, 4, 5, 6, 1}
	if string(bytesOf(got)) != string(want) {
		t.Fatalf("payloads %v, want %v", bytesOf(got), want)
	}
	if <-startedCh != 1 || <-endedCh != 1 {
		t.Fatal("the callbacks were not called for the clip")
	}
}

func TestTheStreamStaysContinuousAcrossAClip(t *testing.T) {
	interludes := &Interludes{}
	got := payloads(t, PlayoutOptions{
		Playlist:   twoTracks(),
		Interludes: interludes,
	}, 9, func() {
		interludes.Offer(&Clip{
			Before: 1,
			Packets: [][]byte{
				{90},
				{91},
			},
		})
	})
	for i, p := range got {
		if p.SequenceNumber != uint16(i) {
			t.Fatalf("packet %d has sequence number %d: the clip made a gap or a repeat", i, p.SequenceNumber)
		}
		if want := uint32(i) * 960; p.Timestamp != want {
			t.Fatalf("packet %d has timestamp %d, want %d", i, p.Timestamp, want)
		}
		if p.Marker != (i == 0) {
			t.Fatalf("packet %d: marker %v", i, p.Marker)
		}
	}
}

func TestAClipWaitsThroughTheStartOfOtherTracksForItsOwn(t *testing.T) {
	interludes := &Interludes{}
	// The first boundary the playout reaches is the start of track a, at the very beginning, and the clip is for track b:
	// it is not played there and not thrown away, and it plays in front of b.
	got := payloads(t, PlayoutOptions{
		Playlist:   twoTracks(),
		Interludes: interludes,
	}, 8, func() {
		interludes.Offer(&Clip{
			Before:  1,
			Packets: [][]byte{{90}},
		})
	})
	if want := []byte{1, 2, 3, 90, 4, 5, 6, 1}; string(bytesOf(got)) != string(want) {
		t.Fatalf("payloads %v, want %v", bytesOf(got), want)
	}
}

func TestANewerClipReplacesTheWaitingOneAndOnlyTheNewerPlays(t *testing.T) {
	interludes := &Interludes{}
	var replaced *Clip
	got := payloads(t, PlayoutOptions{
		Playlist:   twoTracks(),
		Interludes: interludes,
	}, 8, func() {
		interludes.Offer(&Clip{
			Before:  1,
			Packets: [][]byte{{90}},
			Text:    "old",
		})
		replaced = interludes.Offer(&Clip{
			Before:  1,
			Packets: [][]byte{{91}},
			Text:    "new",
		})
	})
	if replaced == nil || replaced.Text != "old" {
		t.Fatalf("Offer returned %+v, want the old clip", replaced)
	}
	if want := []byte{1, 2, 3, 91, 4, 5, 6, 1}; string(bytesOf(got)) != string(want) {
		t.Fatalf("payloads %v, want %v", bytesOf(got), want)
	}
}

func TestOfferIgnoresAClipWithNothingToPlay(t *testing.T) {
	interludes := &Interludes{}
	if interludes.Offer(nil) != nil || interludes.Offer(&Clip{Before: 1}) != nil {
		t.Fatal("an empty clip replaced something")
	}
	if clip := interludes.take(1); clip != nil {
		t.Fatalf("an empty clip was kept: %+v", clip)
	}
}

func TestAClipOfferedWhileAnotherPlaysDoesNotPlayStraightAfterIt(t *testing.T) {
	interludes := &Interludes{}
	var offered bool
	got := payloads(t, PlayoutOptions{
		Playlist:   twoTracks(),
		Interludes: interludes,
		OnClip: func(clip *Clip, event ClipEvent) {
			if event&ClipStarted != 0 && !offered {
				offered = true
				// A second clip for the same track arrives while the first plays.
				interludes.Offer(&Clip{
					Before:  1,
					Packets: [][]byte{{95}},
				})
			}
		},
	}, 9, func() {
		interludes.Offer(&Clip{
			Before: 1,
			Packets: [][]byte{
				{90},
				{91},
			},
		})
	})
	// The first clip, then the track; the second clip waits for track b to come round again, after a.
	if want := []byte{1, 2, 3, 90, 91, 4, 5, 6, 1}; string(bytesOf(got)) != string(want) {
		t.Fatalf("payloads %v, want %v", bytesOf(got), want)
	}
}

func TestSendingAPacketDoesNotAllocate(t *testing.T) {
	p, err := NewPlayout(PlayoutOptions{
		Playlist: tinyPlaylist(),
		Sink:     io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	if allocs := testing.AllocsPerRun(1000, func() { p.send() }); allocs != 0 {
		t.Fatalf("%v allocations for one packet, want none", allocs)
	}
}

// A stallingSink is a collector that blocks once, on its second packet, for stall.
type stallingSink struct {
	collector
	stall   time.Duration
	stalled atomic.Bool
}

func (s *stallingSink) Write(b []byte) (int, error) {
	n, err := s.collector.Write(b)
	if s.len() == 2 && !s.stalled.Swap(true) {
		time.Sleep(s.stall)
	}
	return n, err
}

func TestPlayoutSkipsAudioAfterAStallAndKeepsTheSequenceNumbersContiguous(t *testing.T) {
	sink := &stallingSink{stall: 300 * time.Millisecond}
	playout, err := NewPlayout(PlayoutOptions{
		Playlist: tinyPlaylist(),
		Sink:     sink,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		_ = playout.Do(ctx)
	}()
	waitForPackets(t, &sink.collector, 12)
	cancel()
	<-doneCh

	// A stall of 15 frames is caught up by maxCatchUp packets, and the rest of the audio is skipped.
	skipped := playout.skipped.Load()
	if skipped == 0 {
		t.Fatal("no audio was skipped after a stall of 15 frames")
	}
	packets := sink.packets()
	jumps := 0
	for i, p := range packets {
		if p.SequenceNumber != uint16(i) {
			t.Fatalf("packet %d has sequence %d: the sequence numbers must stay contiguous", i, p.SequenceNumber)
		}
		if i > 0 && p.Timestamp-packets[i-1].Timestamp != 960 {
			jumps++
			if p.Timestamp-packets[i-1].Timestamp != uint32(skipped+1)*960 {
				t.Fatalf("packet %d jumps the RTP clock by %d samples, want %d", i, p.Timestamp-packets[i-1].Timestamp, (skipped+1)*960)
			}
		}
	}
	if jumps != 1 {
		t.Fatalf("%d jumps of the RTP clock, want 1", jumps)
	}
	// The playlist moved past the skipped packets too, so the audio after the jump is later audio.
	last := packets[len(packets)-1]
	if want := byte((uint64(len(packets)-1)+skipped)%3 + 1); last.Payload[0] != want {
		t.Fatalf("the last packet carries %v, want %d: the cursor did not skip the audio", last.Payload, want)
	}
}

func TestPlayoutReportsEachTrackStartAfterItsClip(t *testing.T) {
	sink := &collector{}
	interludes := &Interludes{}
	var starts inmem.List[int]
	playout, err := NewPlayout(PlayoutOptions{
		Playlist:     tinyPlaylist(),
		Sink:         sink,
		Interludes:   interludes,
		OnTrackStart: func(track int) { starts.Add(track) },
	})
	if err != nil {
		t.Fatal(err)
	}
	interludes.Offer(&Clip{
		Before: 0,
		Packets: [][]byte{
			{9},
			{9},
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		_ = playout.Do(ctx)
	}()
	waitForPackets(t, sink, 8)
	cancel()
	<-doneCh

	// The clip plays first, then the track begins; the track of three packets begins again after it.
	got := starts.All()
	if len(got) < 2 || got[0] != 0 || got[1] != 0 {
		t.Fatalf("track starts %v, want the track twice", got)
	}
	packets := sink.packets()
	if packets[0].Payload[0] != 9 || packets[1].Payload[0] != 9 || packets[2].Payload[0] != 1 {
		t.Fatalf("the clip did not play before the track: %v %v %v", packets[0].Payload, packets[1].Payload, packets[2].Payload)
	}
}
