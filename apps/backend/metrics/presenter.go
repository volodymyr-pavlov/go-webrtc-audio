package metrics

import "sync/atomic"

// PresenterMetrics are the counters and timings of the AI presenter, which prepares a spoken introduction for the next
// track while a track plays. Everything in it is atomic.
type PresenterMetrics struct {
	// Prepared counts the clips that were finished in time and offered to the playout.
	Prepared atomic.Uint64

	// Played counts the clips that the playout played.
	Played atomic.Uint64

	// Replaced counts the clips that were replaced by a newer one before they played.
	Replaced atomic.Uint64

	// Late counts the introductions that were not ready before their track began: there was not enough time left, the
	// deadline passed, or the clip was ready after the track had started. The track began without them. A clip that was
	// ready after its track had started is kept and plays when the playlist comes round to that track again.
	Late atomic.Uint64

	// Failed counts the introductions that failed: the model, the voice or the conversion returned an error.
	Failed atomic.Uint64

	// Fallback counts the scripts that were the plain line because the model's text failed its checks.
	Fallback atomic.Uint64

	// ScriptMillis, VoiceMillis and ClipMillis are how long the last introduction took: to write the words, to speak
	// them, and from start to finish, in milliseconds.
	ScriptMillis atomic.Int64
	VoiceMillis  atomic.Int64
	ClipMillis   atomic.Int64
}

// PresenterSnapshot is [PresenterMetrics] at one moment, as the JSON of [Snapshot] holds it.
type PresenterSnapshot struct {
	Prepared     uint64 `json:"prepared"`
	Played       uint64 `json:"played"`
	Replaced     uint64 `json:"replaced"`
	Late         uint64 `json:"late"`
	Failed       uint64 `json:"failed"`
	Fallback     uint64 `json:"fallback"`
	ScriptMillis int64  `json:"script_millis"`
	VoiceMillis  int64  `json:"voice_millis"`
	ClipMillis   int64  `json:"clip_millis"`
}

// snapshot reads every figure once.
func (p *PresenterMetrics) snapshot() PresenterSnapshot {
	return PresenterSnapshot{
		Prepared:     p.Prepared.Load(),
		Played:       p.Played.Load(),
		Replaced:     p.Replaced.Load(),
		Late:         p.Late.Load(),
		Failed:       p.Failed.Load(),
		Fallback:     p.Fallback.Load(),
		ScriptMillis: p.ScriptMillis.Load(),
		VoiceMillis:  p.VoiceMillis.Load(),
		ClipMillis:   p.ClipMillis.Load(),
	}
}
