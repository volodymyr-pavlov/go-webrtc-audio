package playlist

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/pion/webrtc/v4/pkg/media/oggreader"
)

// maxPacketSize is the largest Opus packet a page may hold: one frame of up to 1275 bytes.
const maxPacketSize = 1275

var extensions = []string{
	".aac",
	".flac",
	".m4a",
	".mp3",
	".ogg",
	".opus",
	".wav",
}

// A Track is one audio file as a list of Opus packets, each of them [Playlist.PacketDuration] long.
type Track struct {
	// Name is the file name of the track.
	Name string

	// Packets are the Opus packets of the track, in order. They share one backing array and must not be modified.
	Packets [][]byte
}

// A Playlist is a list of tracks. It never changes after [Load] returns it, so it is safe for concurrent use.
type Playlist struct {
	// Tracks are the tracks in the order they play.
	Tracks []Track

	// PacketDuration is the length of the audio in every packet. Zero means [DefaultFrameDuration].
	PacketDuration time.Duration
}

// Validate returns [ErrEmpty] if the playlist has no tracks or a track has no packets. A nil playlist has none.
func (p *Playlist) Validate() error {
	if p == nil || len(p.Tracks) == 0 {
		return ErrEmpty
	}
	for i := range p.Tracks {
		if len(p.Tracks[i].Packets) == 0 {
			return ErrEmpty
		}
	}
	return nil
}

// Interval returns the length of the audio in every packet.
func (p *Playlist) Interval() time.Duration {
	if p.PacketDuration <= 0 {
		return DefaultFrameDuration
	}
	return p.PacketDuration
}

// SamplesPerPacket returns the number of RTP clock ticks that one packet covers.
func (p *Playlist) SamplesPerPacket() uint32 {
	return uint32(p.Interval() * SampleRate / time.Second)
}

// TrackDuration returns the length of a track.
func (p *Playlist) TrackDuration(track int) time.Duration {
	return time.Duration(len(p.Tracks[track].Packets)) * p.Interval()
}

// Duration returns the length of all tracks together.
func (p *Playlist) Duration() time.Duration {
	var total time.Duration
	for i := range p.Tracks {
		total += p.TrackDuration(i)
	}
	return total
}

// Bytes returns the size of the packets in memory.
func (p *Playlist) Bytes() int {
	var total int
	for i := range p.Tracks {
		for _, packet := range p.Tracks[i].Packets {
			total += len(packet)
		}
	}
	return total
}

// Load reads every audio file in dir, in name order, and returns them as a [Playlist]. It runs ffmpeg once per file
// to make Opus packets of [Options.FrameDuration], so the files may be in any format ffmpeg reads.
//
// It returns [ErrEmpty] if dir has no audio file or a file has no audio, and [ErrBadFrame] for a frame duration other
// than 20, 40 or 60 milliseconds.
func Load(ctx context.Context, dir string, opts Options) (*Playlist, error) {
	opts, err := normalizeFrame(opts)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("playlist: read %s: %w", dir, err)
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if slices.Contains(extensions, strings.ToLower(filepath.Ext(entry.Name()))) {
			names = append(names, entry.Name())
		}
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("%w in %s", ErrEmpty, dir)
	}
	slices.Sort(names)

	list := &Playlist{
		PacketDuration: opts.FrameDuration,
	}
	for _, name := range names {
		track, err := loadTrack(ctx, filepath.Join(dir, name), opts)
		if err != nil {
			return nil, fmt.Errorf("playlist: %s: %w", name, err)
		}
		track.Name = name
		list.Tracks = append(list.Tracks, track)
	}
	return list, nil
}

// LoadTrack reads the one audio file at path and returns it as a [Track] of Opus packets of [Options.FrameDuration]: a
// way to turn a clip that was just made, such as a spoken introduction, into packets that a [Playlist] can play. The
// name of the track is the name of the file. It returns [ErrBadFrame] for a frame duration other than 20, 40 or 60
// milliseconds, and [ErrEmpty] if the file has no audio.
func LoadTrack(ctx context.Context, path string, opts Options) (Track, error) {
	opts, err := normalizeFrame(opts)
	if err != nil {
		return Track{}, err
	}
	track, err := loadTrack(ctx, path, opts)
	if err != nil {
		return Track{}, fmt.Errorf("playlist: %s: %w", filepath.Base(path), err)
	}
	track.Name = filepath.Base(path)
	return track, nil
}

// normalizeFrame returns opts with the frame duration set to [DefaultFrameDuration] if it is zero. It returns
// [ErrBadFrame] for a duration other than 20, 40 or 60 milliseconds.
func normalizeFrame(opts Options) (Options, error) {
	if opts.FrameDuration == 0 {
		opts.FrameDuration = DefaultFrameDuration
	}
	switch opts.FrameDuration {
	case 20 * time.Millisecond, 40 * time.Millisecond, 60 * time.Millisecond:
		return opts, nil
	}
	return opts, ErrBadFrame
}

// loadTrack runs ffmpeg on the file at path and returns the Opus packets of opts.FrameDuration, which the caller has
// already checked. The returned track has no name.
func loadTrack(ctx context.Context, path string, opts Options) (Track, error) {
	program := opts.FFmpeg
	if program == "" {
		program = "ffmpeg"
	}
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, program,
		"-v", "error",
		"-i", path,
		"-vn",
		"-ac", "2",
		"-ar", strconv.Itoa(SampleRate),
		"-c:a", "libopus",
		"-b:a", strconv.Itoa(Bitrate),
		"-frame_duration", strconv.Itoa(int(opts.FrameDuration/time.Millisecond)),
		"-page_duration", strconv.Itoa(int(opts.FrameDuration/time.Microsecond)),
		"-f", "ogg",
		"pipe:1",
	)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return Track{}, fmt.Errorf("ffmpeg: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return readOgg(&stdout, uint64(opts.FrameDuration*SampleRate/time.Second))
}

// readOgg reads the Opus packets of an Ogg stream whose pages each hold one packet of samples samples. Besides the size
// of the page it checks the granule position, which grows by the length of the audio in a page: a page that holds two
// packets grows it by twice as much and is refused. The first audio page is not checked, because its position includes
// the pre-skip of the encoder, and the last may be shorter than the others, because the encoder cuts the end of the
// audio off.
func readOgg(in io.Reader, samples uint64) (Track, error) {
	reader, _, err := oggreader.NewWith(in)
	if err != nil {
		return Track{}, err
	}
	var backing []byte
	var ends []int
	var previous uint64
	var short bool
	for {
		payload, header, err := reader.ParseNextPage()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Track{}, err
		}
		if _, ok := header.HeaderType(payload); ok {
			continue
		}
		if bytes.HasPrefix(payload, []byte("OpusTags")) {
			continue
		}
		if len(payload) == 0 || len(payload) > maxPacketSize {
			return Track{}, ErrBadPacket
		}
		if short {
			// A page that was not the last one is shorter or longer than a packet.
			return Track{}, ErrBadPacket
		}
		if len(ends) > 0 {
			delta := header.GranulePosition - previous
			switch {
			case header.GranulePosition < previous || delta > samples:
				return Track{}, ErrBadPacket
			case delta < samples:
				short = true
			}
		}
		previous = header.GranulePosition
		backing = append(backing, payload...)
		ends = append(ends, len(backing))
	}
	if len(ends) == 0 {
		return Track{}, ErrEmpty
	}
	track := Track{
		Packets: make([][]byte, len(ends)),
	}
	start := 0
	for i, end := range ends {
		track.Packets[i] = backing[start:end:end]
		start = end
	}
	return track, nil
}

// A Cursor walks over a [Playlist] one packet at a time and starts again after the last one. Its position is one atomic
// counter, so it is safe for concurrent use: every call gets the next packet, whoever makes it.
type Cursor struct {
	list *Playlist
	// ends[i] is the number of packets in tracks 0 to i, so the packet with number n of a loop is in the first track
	// whose end is above n.
	ends []int
	next atomic.Uint64
}

// NewCursor returns a [Cursor] at the first packet of the first track.
func NewCursor(list *Playlist) *Cursor {
	c := &Cursor{
		list: list,
		ends: make([]int, len(list.Tracks)),
	}
	total := 0
	for i := range list.Tracks {
		total += len(list.Tracks[i].Packets)
		c.ends[i] = total
	}
	return c
}

// locate returns the track of the packet that the loop position pos selects and the index of that packet in the
// track, and whether the playlist has a packet at all. It does not move the cursor.
func (c *Cursor) locate(pos uint64) (int, int, bool) {
	if len(c.ends) == 0 || c.ends[len(c.ends)-1] == 0 {
		return 0, 0, false
	}
	n := int(pos % uint64(c.ends[len(c.ends)-1]))
	track, _ := slices.BinarySearch(c.ends, n+1)
	start := 0
	if track > 0 {
		start = c.ends[track-1]
	}
	return track, n - start, true
}

// AtTrackStart reports whether the packet that [Cursor.Next] would return now is the first of a track, and which track
// it belongs to. Only one goroutine may use it together with Next, because another could move the cursor between the
// two.
func (c *Cursor) AtTrackStart() (int, bool) {
	track, index, found := c.locate(c.next.Load())
	return track, found && index == 0
}

// Next returns the next packet and the index of its track. It returns a nil packet if the playlist has no packet.
func (c *Cursor) Next() ([]byte, int) {
	track, index, ok := c.locate(c.next.Add(1) - 1)
	if !ok {
		return nil, 0
	}
	return c.list.Tracks[track].Packets[index], track
}
