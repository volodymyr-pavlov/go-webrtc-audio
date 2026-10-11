package stream

import "sync/atomic"

// A Clip is a short piece of audio that plays once, in front of one track: the words of a presenter who introduces it.
// The packets must have the frame duration of the playlist and the sound of its other packets (48 kHz stereo Opus), or
// the clip will play at the wrong speed.
type Clip struct {
	// Before is the index of the track that the clip comes before. The playout plays the clip when that track is about to
	// start, and until then the clip waits, through the starts of other tracks, unless a newer clip takes its place.
	Before int

	// Packets are the Opus packets of the clip, in order. A clip with none is never played.
	Packets [][]byte

	// Text is what the clip says, for the page to show.
	Text string
}

// Interludes holds the clip that waits for the next track to start. It has one place: a clip offered while another
// waits replaces it, because a clip that was made later was made with later facts and is the one worth playing. It is
// safe for concurrent use, and nothing in it takes a lock, so the playout is never held up by the goroutine that makes
// the clips.
type Interludes struct {
	waiting atomic.Pointer[Clip]
}

// Offer puts clip in the place of the waiting one and returns the clip it replaced, or nil. A nil clip, or one without
// packets, is ignored and nil is returned.
func (i *Interludes) Offer(clip *Clip) *Clip {
	if clip == nil || len(clip.Packets) == 0 {
		return nil
	}
	return i.waiting.Swap(clip)
}

// Waiting returns the clip that waits for its track, or nil. It does not remove it.
func (i *Interludes) Waiting() *Clip {
	return i.waiting.Load()
}

// take removes the waiting clip if it comes before the track that is about to start, and returns it. A clip for another
// track stays where it is and waits for its own: a presenter that prepares more than one track ahead has clips for
// later boundaries, and one that was late has a clip for a boundary that comes round again with the loop. A clip that
// was replaced while take ran is not returned; the newer one waits for its turn.
func (i *Interludes) take(next int) *Clip {
	waiting := i.waiting.Load()
	if waiting == nil || waiting.Before != next {
		return nil
	}
	if !i.waiting.CompareAndSwap(waiting, nil) {
		return nil
	}
	return waiting
}
