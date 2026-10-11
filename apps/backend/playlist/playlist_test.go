package playlist

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func makeTone(t *testing.T, dir, name string, seconds string) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is not installed")
	}
	out, err := exec.Command("ffmpeg", "-v", "error", "-y", "-f", "lavfi", "-t", seconds,
		"-i", "sine=frequency=440:sample_rate=48000", "-ac", "2", filepath.Join(dir, name)).CombinedOutput()
	if err != nil {
		t.Fatalf("ffmpeg: %v: %s", err, out)
	}
}

func TestLoadMakesTwentyMillisecondPackets(t *testing.T) {
	dir := t.TempDir()
	makeTone(t, dir, "b.wav", "2")
	makeTone(t, dir, "a.wav", "1")

	list, err := Load(context.Background(), dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Tracks) != 2 || list.Tracks[0].Name != "a.wav" || list.Tracks[1].Name != "b.wav" {
		t.Fatalf("tracks are not in name order: %+v", list.Tracks)
	}
	for _, track := range list.Tracks {
		want := map[string]time.Duration{
			"a.wav": time.Second,
			"b.wav": 2 * time.Second,
		}[track.Name]
		index := 0
		if track.Name == "b.wav" {
			index = 1
		}
		if got := list.TrackDuration(index); got < want-60*time.Millisecond || got > want+60*time.Millisecond {
			t.Errorf("%s lasts %v, want about %v", track.Name, got, want)
		}
	}
}

func TestLoadRejectsAnEmptyDirectory(t *testing.T) {
	_, err := Load(context.Background(), t.TempDir(), Options{})
	if !errors.Is(err, ErrEmpty) {
		t.Fatalf("got %v, want ErrEmpty", err)
	}
}

func TestCursorWrapsAround(t *testing.T) {
	list := &Playlist{
		Tracks: []Track{
			{
				Name: "a",
				Packets: [][]byte{
					{1},
					{2},
				},
			},
			{
				Name:    "b",
				Packets: [][]byte{{3}},
			},
		},
	}
	cursor := NewCursor(list)
	got := make([]byte, 0, 7)
	tracks := make([]int, 0, 7)
	for range 7 {
		packet, track := cursor.Next()
		got = append(got, packet[0])
		tracks = append(tracks, track)
	}
	if string(got) != string([]byte{1, 2, 3, 1, 2, 3, 1}) {
		t.Errorf("packets %v", got)
	}
	if tracks[2] != 1 || tracks[3] != 0 {
		t.Errorf("tracks %v", tracks)
	}
}

func TestLongerFramesMakeFewerPackets(t *testing.T) {
	dir := t.TempDir()
	makeTone(t, dir, "a.wav", "3")
	counts := map[time.Duration]int{}
	for _, frame := range []time.Duration{20 * time.Millisecond, 40 * time.Millisecond, 60 * time.Millisecond} {
		list, err := Load(context.Background(), dir, Options{FrameDuration: frame})
		if err != nil {
			t.Fatal(err)
		}
		if list.Interval() != frame {
			t.Fatalf("Interval = %v, want %v", list.Interval(), frame)
		}
		if want := uint32(frame * SampleRate / time.Second); list.SamplesPerPacket() != want {
			t.Fatalf("SamplesPerPacket = %d, want %d", list.SamplesPerPacket(), want)
		}
		got := list.Duration()
		if got < 3*time.Second-100*time.Millisecond || got > 3*time.Second+100*time.Millisecond {
			t.Fatalf("a 3 s file lasts %v with %v frames", got, frame)
		}
		counts[frame] = len(list.Tracks[0].Packets)
	}
	if counts[20*time.Millisecond] < 2*counts[40*time.Millisecond]-2 || counts[20*time.Millisecond] < 3*counts[60*time.Millisecond]-3 {
		t.Fatalf("packet counts do not fall with the frame length: %v", counts)
	}
}

func TestLoadRejectsAnOddFrameDuration(t *testing.T) {
	dir := t.TempDir()
	makeTone(t, dir, "a.wav", "1")
	if _, err := Load(context.Background(), dir, Options{FrameDuration: 30 * time.Millisecond}); !errors.Is(err, ErrBadFrame) {
		t.Fatalf("got %v, want ErrBadFrame", err)
	}
}

func TestCursorIsSafeForConcurrentUse(t *testing.T) {
	list := &Playlist{
		Tracks: []Track{
			{
				Name: "a",
				Packets: [][]byte{
					{0},
					{1},
					{2},
				},
			},
			{
				Name: "b",
				Packets: [][]byte{
					{3},
					{4},
				},
			},
		},
	}
	cursor := NewCursor(list)
	const workers, each = 8, 1000
	var counts [5]atomic.Int64
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range each {
				packet, track := cursor.Next()
				if (packet[0] < 3) != (track == 0) {
					t.Errorf("packet %d reported as track %d", packet[0], track)
				}
				counts[packet[0]].Add(1)
			}
		}()
	}
	wg.Wait()
	// 8000 packets over a loop of 5: every packet comes up the same number of times.
	for i := range counts {
		if got := counts[i].Load(); got != workers*each/5 {
			t.Errorf("packet %d came up %d times, want %d", i, got, workers*each/5)
		}
	}
}

func TestCursorOfAnEmptyPlaylistReturnsNothing(t *testing.T) {
	if packet, _ := NewCursor(&Playlist{}).Next(); packet != nil {
		t.Fatalf("got %v", packet)
	}
}

// oggOf returns the Ogg Opus of a one-second tone, with pages of pageMicroseconds of audio.
func oggOf(t *testing.T, frame, page int) []byte {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is not installed")
	}
	out, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-t", "1",
		"-i", "sine=frequency=440:sample_rate=48000", "-ac", "2", "-c:a", "libopus",
		"-frame_duration", strconv.Itoa(frame), "-page_duration", strconv.Itoa(page*1000), "-f", "ogg", "pipe:1").Output()
	if err != nil {
		t.Fatalf("ffmpeg: %v", err)
	}
	return out
}

func TestReadOggTakesPagesOfOnePacket(t *testing.T) {
	track, err := readOgg(bytes.NewReader(oggOf(t, 20, 20)), 960)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(track.Packets); n < 49 || n > 52 {
		t.Fatalf("%d packets in a second of 20 ms, want about 50", n)
	}
}

func TestReadOggRefusesAPageOfTwoPackets(t *testing.T) {
	// Pages of 40 ms hold two packets of 20 ms: the page is one block of bytes of an allowed size, and only the
	// granule position shows that it is two packets.
	_, err := readOgg(bytes.NewReader(oggOf(t, 20, 40)), 960)
	if !errors.Is(err, ErrBadPacket) {
		t.Fatalf("got %v, want ErrBadPacket", err)
	}
}

func TestAtTrackStartSaysWhenTheNextPacketOpensATrack(t *testing.T) {
	cursor := NewCursor(&Playlist{Tracks: []Track{
		{
			Name: "a",
			Packets: [][]byte{
				{1},
				{2},
			},
		},
		{
			Name: "b",
			Packets: [][]byte{
				{3},
				{4},
				{5},
			},
		},
	}})
	// Five packets, then round again: the starts are the first packet of a, the third packet (b), and the sixth (a again).
	type step struct {
		track int
		start bool
	}
	want := []step{
		{0, true},
		{0, false},
		{1, true},
		{1, false},
		{1, false},
		{0, true},
		{0, false},
	}
	for i, w := range want {
		track, start := cursor.AtTrackStart()
		if track != w.track || start != w.start {
			t.Fatalf("before packet %d: track %d, start %v; want track %d, start %v", i, track, start, w.track, w.start)
		}
		if _, got := cursor.Next(); got != w.track {
			t.Fatalf("packet %d belongs to track %d, want %d", i, got, w.track)
		}
	}
}

func TestAtTrackStartOfAnEmptyPlaylistIsNever(t *testing.T) {
	if _, start := NewCursor(&Playlist{}).AtTrackStart(); start {
		t.Fatal("an empty playlist has a track start")
	}
}

func TestLoadTrackMakesPacketsOfTheFrameDurationFromOneFile(t *testing.T) {
	dir := t.TempDir()
	makeTone(t, dir, "clip.wav", "1")
	for frame, want := range map[time.Duration]int{
		20 * time.Millisecond: 50,
		60 * time.Millisecond: 17,
	} {
		track, err := LoadTrack(context.Background(), filepath.Join(dir, "clip.wav"), Options{FrameDuration: frame})
		if err != nil {
			t.Fatal(err)
		}
		if track.Name != "clip.wav" || len(track.Packets) < want-1 || len(track.Packets) > want+2 {
			t.Fatalf("%v: %q has %d packets, want about %d", frame, track.Name, len(track.Packets), want)
		}
	}
	if _, err := LoadTrack(context.Background(), filepath.Join(dir, "clip.wav"), Options{FrameDuration: 30 * time.Millisecond}); !errors.Is(err, ErrBadFrame) {
		t.Fatalf("got %v, want ErrBadFrame", err)
	}
	if _, err := LoadTrack(context.Background(), filepath.Join(dir, "missing.wav"), Options{}); err == nil {
		t.Fatal("a missing file was loaded")
	}
}
