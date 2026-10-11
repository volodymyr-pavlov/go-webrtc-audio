package metrics

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
)

func TestParseSnapshotReadsWhatTheEncoderWrites(t *testing.T) {
	want := Snapshot{
		UptimeSeconds: 12.5,
		Listeners:     7,
		Joined:        9,
		Left:          2,
		Packets:       100,
		Delivered:     600,
		Dropped:       3,
		Evicted:       1,
		WriteErrors:   4,
		UDPSends:      50,
		UDPPackets:    600,
		Goroutines:    130,
		HeapBytes:     1 << 20,
		RSSBytes:      5 << 20,
		GCCycles:      8,
		CPUSeconds:    1.25,
		Presenter: PresenterSnapshot{
			Prepared:     5,
			Played:       4,
			Replaced:     1,
			Late:         2,
			Failed:       3,
			Fallback:     1,
			ScriptMillis: 2700,
			VoiceMillis:  900,
			ClipMillis:   4000,
		},
		Runtime: RuntimeSample{
			Time:                   1790000000,
			Goroutines:             130,
			Procs:                  14,
			HeapBytes:              1 << 20,
			StackBytes:             1 << 19,
			TotalBytes:             9 << 20,
			HeapGoalBytes:          4 << 20,
			GCCycles:               8,
			AllocBytesPerSecond:    2048.5,
			GCCPUPercent:           0.25,
			MutexWaitSeconds:       0.001,
			SchedLatencyP99Seconds: 0.002,
			SchedLatencyMaxSeconds: 0.004,
			GCPauseP99Seconds:      0.0005,
			GCPauseMaxSeconds:      0.001,
		},
	}
	var encoded bytes.Buffer
	if err := json.NewEncoder(&encoded).Encode(want); err != nil {
		t.Fatal(err)
	}

	got, err := ParseSnapshot(encoded.Bytes())

	if err != nil || got != want {
		t.Fatalf("got %+v, %v\nwant %+v", got, err, want)
	}
}

func TestParseSnapshotTakesMissingFiguresAsZeroAndRefusesWhatIsNotAnObject(t *testing.T) {
	got, err := ParseSnapshot([]byte(`{"listeners": 3}`))
	if err != nil || got.Listeners != 3 || got.Packets != 0 {
		t.Fatalf("got %+v, %v", got, err)
	}
	for _, bad := range []string{``, `[]`, `3`, `{"listeners": `, `{} {}`} {
		if _, err := ParseSnapshot([]byte(bad)); !errors.Is(err, ErrBadSnapshot) {
			t.Errorf("%q: got %v, want ErrBadSnapshot", bad, err)
		}
	}
}
