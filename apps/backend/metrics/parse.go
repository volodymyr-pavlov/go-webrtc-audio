package metrics

import (
	"github.com/tidwall/gjson"
)

// ParseSnapshot reads the JSON that the metrics handler of the API writes. A figure that is missing, or that is not a number, is 0.
//
// It returns [ErrBadSnapshot] if encoded is not a JSON object.
func ParseSnapshot(encoded []byte) (Snapshot, error) {
	root := gjson.ParseBytes(encoded)
	if !root.IsObject() || !gjson.ValidBytes(encoded) {
		return Snapshot{}, ErrBadSnapshot
	}
	presenter := root.Get("presenter")
	return Snapshot{
		UptimeSeconds: root.Get("uptime_seconds").Float(),
		Listeners:     root.Get("listeners").Int(),
		Joined:        root.Get("joined").Uint(),
		Left:          root.Get("left").Uint(),
		Packets:       root.Get("packets").Uint(),
		Delivered:     root.Get("delivered").Uint(),
		Dropped:       root.Get("dropped").Uint(),
		Evicted:       root.Get("evicted").Uint(),
		WriteErrors:   root.Get("write_errors").Uint(),
		UDPSends:      root.Get("udp_sends").Uint(),
		UDPPackets:    root.Get("udp_packets").Uint(),
		Goroutines:    int(root.Get("goroutines").Int()),
		HeapBytes:     root.Get("heap_bytes").Uint(),
		RSSBytes:      root.Get("rss_bytes").Uint(),
		GCCycles:      uint32(root.Get("gc_cycles").Uint()),
		Presenter: PresenterSnapshot{
			Prepared:     presenter.Get("prepared").Uint(),
			Played:       presenter.Get("played").Uint(),
			Replaced:     presenter.Get("replaced").Uint(),
			Late:         presenter.Get("late").Uint(),
			Failed:       presenter.Get("failed").Uint(),
			Fallback:     presenter.Get("fallback").Uint(),
			ScriptMillis: presenter.Get("script_millis").Int(),
			VoiceMillis:  presenter.Get("voice_millis").Int(),
			ClipMillis:   presenter.Get("clip_millis").Int(),
		},
		Runtime:    parseRuntimeSample(root.Get("runtime")),
		CPUSeconds: root.Get("cpu_seconds").Float(),
	}, nil
}

// parseRuntimeSample reads a [RuntimeSample] from its JSON object.
func parseRuntimeSample(sample gjson.Result) RuntimeSample {
	return RuntimeSample{
		Time:                   sample.Get("time").Int(),
		Goroutines:             sample.Get("goroutines").Uint(),
		Procs:                  sample.Get("procs").Uint(),
		HeapBytes:              sample.Get("heap_bytes").Uint(),
		StackBytes:             sample.Get("stack_bytes").Uint(),
		TotalBytes:             sample.Get("total_bytes").Uint(),
		HeapGoalBytes:          sample.Get("heap_goal_bytes").Uint(),
		GCCycles:               sample.Get("gc_cycles").Uint(),
		AllocBytesPerSecond:    sample.Get("alloc_bytes_per_second").Float(),
		GCCPUPercent:           sample.Get("gc_cpu_percent").Float(),
		MutexWaitSeconds:       sample.Get("mutex_wait_seconds").Float(),
		SchedLatencyP99Seconds: sample.Get("sched_latency_p99_seconds").Float(),
		SchedLatencyMaxSeconds: sample.Get("sched_latency_max_seconds").Float(),
		GCPauseP99Seconds:      sample.Get("gc_pause_p99_seconds").Float(),
		GCPauseMaxSeconds:      sample.Get("gc_pause_max_seconds").Float(),
	}
}
