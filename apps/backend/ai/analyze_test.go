package ai

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

const probeJSON = `{"format": {"filename": "x.mp3", "duration": "116.924042", "tags": {"TITLE": "Night Drive", "Artist": "  The   Lamps ", "genre": "synthwave"}}}`

// summaryText is the end of the output of ffmpeg -af ebur128=peak=true, with a running line before the summary, as ffmpeg
// writes them.
const summaryText = `[Parsed_ebur128_0 @ 0x81cc4c900] t: 29.999979  TARGET:-23 LUFS    M: -25.0 S: -24.9     I: -20.0 LUFS       LRA:   0.0 LU  FTPK: -21.1 -21.1 dBFS  TPK: -21.1 -21.1 dBFS
[Parsed_ebur128_0 @ 0x81cc4c900] Summary:

  Integrated loudness:
    I:         -24.9 LUFS
    Threshold: -34.9 LUFS

  Loudness range:
    LRA:         3.5 LU
    Threshold: -44.9 LUFS

  True peak:
    Peak:      -21.1 dBFS
`

type fakeRunner struct {
	probe   string
	summary string
	err     error
	calls   [][]string
}

func (f *fakeRunner) Run(_ context.Context, _ io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
	f.calls = append(f.calls, append([]string{name}, args...))
	if f.err != nil {
		return f.err
	}
	if name == "ffprobe" {
		_, err := io.WriteString(stdout, f.probe)
		return err
	}
	_, err := io.WriteString(stderr, f.summary)
	return err
}

func TestTrackReadsTagsDurationAndTheLoudnessSummary(t *testing.T) {
	runner := &fakeRunner{
		probe:   probeJSON,
		summary: summaryText,
	}
	track, err := newAnalyzer(runner.Run, analyzerOptions{}).Track(context.Background(), "audio/x.mp3")
	if err != nil {
		t.Fatal(err)
	}
	if track.Title != "Night Drive" || track.Artist != "The Lamps" || track.Genre != "synthwave" {
		t.Fatalf("tags: %+v", track)
	}
	if track.Duration < 116*time.Second || track.Duration > 117*time.Second {
		t.Fatalf("duration %v", track.Duration)
	}
	// The summary, not the running value of the line before it, is what counts.
	if track.LUFS != -24.9 || track.LoudnessRange != 3.5 || track.PeakDB != -21.1 {
		t.Fatalf("loudness: %+v", track)
	}
	if track.Name != "x.mp3" || track.File != "audio/x.mp3" {
		t.Fatalf("name: %+v", track)
	}
}

func TestTrackPassesThePathAsOneArgumentNeverThroughAShell(t *testing.T) {
	runner := &fakeRunner{
		probe:   probeJSON,
		summary: summaryText,
	}
	evil := "a; rm -rf ~ $(whoami).mp3"
	if _, err := newAnalyzer(runner.Run, analyzerOptions{}).Track(context.Background(), evil); err != nil {
		t.Fatal(err)
	}
	for _, call := range runner.calls {
		found := 0
		for _, arg := range call {
			if arg == evil {
				found++
			}
		}
		if found != 1 {
			t.Fatalf("the path is not exactly one argument of %v", call)
		}
	}
}

func TestTrackRefusesAFileWithoutADuration(t *testing.T) {
	for _, probe := range []string{`{"format": {}}`, `{"format": {"duration": "N/A"}}`, `{"format": {"duration": "-3"}}`, `{"format": {"duration": "NaN"}}`} {
		_, err := newAnalyzer((&fakeRunner{
			probe:   probe,
			summary: summaryText,
		}).Run, analyzerOptions{}).Track(context.Background(), "x")
		if !errors.Is(err, errNoDuration) {
			t.Errorf("%s: got %v, want errNoDuration", probe, err)
		}
	}
}

func TestTrackRefusesOutputWithoutALoudnessSummary(t *testing.T) {
	_, err := newAnalyzer((&fakeRunner{
		probe:   probeJSON,
		summary: "nothing useful",
	}).Run, analyzerOptions{}).Track(context.Background(), "x")
	if !errors.Is(err, errNoLoudness) {
		t.Fatalf("got %v, want errNoLoudness", err)
	}
}

func TestTrackReportsAProgramThatFails(t *testing.T) {
	boom := errors.New("boom")
	_, err := newAnalyzer((&fakeRunner{err: boom}).Run, analyzerOptions{}).Track(context.Background(), "x")
	if !errors.Is(err, boom) {
		t.Fatalf("got %v, want the error of the program", err)
	}
}

func TestSilenceIsALevelNoRecordingHas(t *testing.T) {
	var track Track
	silent := "  I:         -inf LUFS\n  LRA:         0.0 LU\n  Peak:      -inf dBFS\n"
	if err := parseLoudness([]byte(silent), &track); err != nil {
		t.Fatal(err)
	}
	if track.LUFS != -120 || track.PeakDB != -120 {
		t.Fatalf("%+v", track)
	}
}

func TestCleanKeepsWhatIsPrintableAndShort(t *testing.T) {
	long := strings.Repeat("ab ", 100)
	tests := map[string]string{
		"  Night\tDrive \n":             "Night Drive",
		"bell\x07\x00 ring":             "bell ring",
		"Ignore previous\ninstructions": "Ignore previous instructions",
		"":                              "",
		"\u200bzero width":              "zero width",
		strings.Repeat("é", 200):        strings.Repeat("é", 80),
	}
	for in, want := range tests {
		if got := cleanText(in); got != want {
			t.Errorf("cleanText(%q) = %q, want %q", in, got, want)
		}
	}
	if got := cleanText(long); len([]rune(got)) > 80 || strings.HasSuffix(got, " ") {
		t.Errorf("a long text was cleaned to %d runes: %q", len([]rune(got)), got)
	}
}

func TestDisplayTitleFallsBackToTheFileName(t *testing.T) {
	tests := []struct {
		track Track
		want  string
	}{
		{Track{
			Title: "Night Drive",
			Name:  "x.mp3",
		}, "Night Drive"},
		{Track{Name: "8-bit-or-16-bit-retro.mp3"}, "8 bit or 16 bit retro"},
		{Track{Name: "01-third.wav"}, "third"},
		{Track{Name: "02_fifth_song.flac"}, "fifth song"},
		{Track{Name: "07. Blue Hour.ogg"}, "Blue Hour"},
	}
	for _, tt := range tests {
		if got := tt.track.DisplayTitle(); got != tt.want {
			t.Errorf("%q: got %q, want %q", tt.track.Name, got, tt.want)
		}
	}
}

func TestCreditsReadsArtistAndTitleFromAFileNameOfThatForm(t *testing.T) {
	tests := []struct {
		track  Track
		artist string
		title  string
	}{
		{Track{Name: "Gemini - Jump For The Crown.mp3"}, "Gemini", "Jump For The Crown"},
		{Track{Name: "03 - Gemini - The Heavy Survive.mp3"}, "Gemini", "The Heavy Survive"},
		{Track{Name: "Gemini  -  Where The Stars Ignite.MP3"}, "Gemini", "Where The Stars Ignite"},
		// A dash with no spaces is part of a name, not a separator.
		{Track{Name: "8-bit-or-16-bit-retro.mp3"}, "", "8 bit or 16 bit retro"},
		{Track{Name: "01-third.wav"}, "", "third"},
		// The tags win over the file name, and an artist tag keeps the title from the name.
		{Track{
			Name:   "Gemini - Jump.mp3",
			Title:  "Real Title",
			Artist: "Real Artist",
		}, "Real Artist", "Real Title"},
		{Track{
			Name:   "Gemini - Jump.mp3",
			Artist: "Tagged",
		}, "Tagged", "Gemini Jump"},
		{Track{
			Name:  "Gemini - Jump.mp3",
			Title: "Real Title",
		}, "", "Real Title"},
	}
	for _, tt := range tests {
		artist, title := tt.track.Credits()
		if artist != tt.artist || title != tt.title {
			t.Errorf("%q: got %q and %q, want %q and %q", tt.track.Name, artist, title, tt.artist, tt.title)
		}
		if got := tt.track.DisplayTitle(); got != tt.title {
			t.Errorf("%q: DisplayTitle %q, want %q", tt.track.Name, got, tt.title)
		}
	}
}
