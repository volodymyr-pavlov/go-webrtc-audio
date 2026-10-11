package ai

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/tidwall/gjson"
)

const (
	// maxFieldLength is the longest tag kept, in runes. A real title or artist is shorter; a longer one is more likely to
	// be a message to a model.
	maxFieldLength = 80
)

// analyzerOptions describe an [Analyzer].
type analyzerOptions struct {
	// FFmpeg and FFprobe are the programs to start. The defaults are "ffmpeg" and "ffprobe".
	FFmpeg  string
	FFprobe string
}

// A Track is what is known about one audio file.
type Track struct {
	// File is the path of the file, and Name its name without the directory.
	File string
	Name string

	// Title, Artist, Album and Genre are the tags in the file, cleaned with [cleanText]. Any may be empty.
	Title  string
	Artist string
	Album  string
	Genre  string

	// Duration is the length of the audio.
	Duration time.Duration

	// LUFS is the integrated loudness, LoudnessRange the spread of the loudness in loudness units, and PeakDB the
	// highest sample in decibels below full scale (so a negative number).
	LUFS          float64
	LoudnessRange float64
	PeakDB        float64
}

// artistTitle matches a file name of the form "Artist - Title", with spaces around the dash, which is how many files
// that carry no tags are named. A name with a dash and no spaces, such as "8-bit-retro", is not one.
var artistTitle = regexp.MustCompile(`^(.+?)\s+-\s+(.+)$`)

// Credits returns the artist and the title to show and to speak. The tags win. When the file has no title, a file name
// of the form "Artist - Title", after any track number, is read as both; any other name, with its dashes and
// underscores read as spaces, is the title, and the artist is whatever the tags say, which may be nothing.
func (t Track) Credits() (string, string) {
	if t.Title != "" {
		return t.Artist, t.Title
	}
	name := strings.TrimSuffix(t.Name, filepath.Ext(t.Name))
	name = leadingNumber.ReplaceAllString(name, "")
	if t.Artist == "" {
		if m := artistTitle.FindStringSubmatch(name); m != nil {
			return cleanText(m[1]), cleanText(m[2])
		}
	}
	return t.Artist, cleanText(strings.NewReplacer("-", " ", "_", " ").Replace(name))
}

// DisplayTitle returns the title to show and to speak, which [Track.Credits] works out.
func (t Track) DisplayTitle() string {
	_, title := t.Credits()
	return title
}

// An Analyzer reads [Track]s with ffprobe and ffmpeg.
type Analyzer struct {
	runner  Runner
	ffmpeg  string
	ffprobe string
}

// newAnalyzer returns an [Analyzer] that starts its programs with runner.
func newAnalyzer(runner Runner, opts analyzerOptions) *Analyzer {
	a := &Analyzer{
		runner:  runner,
		ffmpeg:  opts.FFmpeg,
		ffprobe: opts.FFprobe,
	}
	if a.ffmpeg == "" {
		a.ffmpeg = defaultFFmpeg
	}
	if a.ffprobe == "" {
		a.ffprobe = defaultFFprobe
	}
	return a
}

// Track analyzes the file at path. It returns [errNoDuration] for a file that is not audio and [errNoLoudness] if
// ffmpeg did not report a loudness.
func (a *Analyzer) Track(ctx context.Context, path string) (Track, error) {
	var probe bytes.Buffer
	if err := a.runner(ctx, nil, &probe, nil, a.ffprobe,
		"-v", "error", "-print_format", "json", "-show_format", path); err != nil {
		return Track{}, fmt.Errorf("analyze: %s: %w", filepath.Base(path), err)
	}
	track, err := parseProbe(&probe)
	if err != nil {
		return Track{}, fmt.Errorf("analyze: %s: %w", filepath.Base(path), err)
	}
	track.File = path
	track.Name = filepath.Base(path)

	var summary bytes.Buffer
	if err := a.runner(ctx, nil, nil, &summary, a.ffmpeg,
		"-nostats", "-hide_banner", "-i", path, "-af", "ebur128=peak=true", "-f", "null", "-"); err != nil {
		return Track{}, fmt.Errorf("analyze: %s: %w", filepath.Base(path), err)
	}
	if err := parseLoudness(summary.Bytes(), &track); err != nil {
		return Track{}, fmt.Errorf("analyze: %s: %w", filepath.Base(path), err)
	}
	return track, nil
}

// parseProbe reads the JSON that ffprobe writes to r. Tag names are matched without regard to case, because formats
// differ in how they write them.
func parseProbe(r io.Reader) (Track, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return Track{}, fmt.Errorf("read the output of ffprobe: %w", err)
	}
	if !gjson.ValidBytes(data) {
		return Track{}, fmt.Errorf("read the output of ffprobe: %w", errBadProbe)
	}
	format := gjson.GetBytes(data, "format")
	seconds, err := strconv.ParseFloat(format.Get("duration").String(), 64)
	if err != nil || seconds <= 0 || math.IsInf(seconds, 0) || math.IsNaN(seconds) {
		return Track{}, errNoDuration
	}
	tags := map[string]string{}
	format.Get("tags").ForEach(func(key, value gjson.Result) bool {
		tags[strings.ToLower(key.String())] = value.String()
		return true
	})
	return Track{
		Title:    cleanText(tags["title"]),
		Artist:   cleanText(tags["artist"]),
		Album:    cleanText(tags["album"]),
		Genre:    cleanText(tags["genre"]),
		Duration: time.Duration(seconds * float64(time.Second)),
	}, nil
}

// leadingNumber matches a track number at the start of a file name: two or three digits and a separator, as in "01-",
// or digits, a dot and a space, as in "7. ". One digit and a dash is not one, because that is how "8-bit" starts.
var leadingNumber = regexp.MustCompile(`^(?:\d{2,3}\s*[-_.]\s*|\d{1,3}\.\s+)`)

var integratedPattern = regexp.MustCompile(`(?m)^\s*I:\s+(-?\d+(?:\.\d+)?|-inf)\s+LUFS`)
var rangePattern = regexp.MustCompile(`(?m)^\s*LRA:\s+(-?\d+(?:\.\d+)?)\s+LU`)
var peakPattern = regexp.MustCompile(`(?m)^\s*Peak:\s+(-?\d+(?:\.\d+)?|-inf)\s+dBFS`)

// parseLoudness reads the summary that ebur128 writes at the end of its output into track. It takes the last match of
// each figure, because the filter also logs a running value of the same kind while it works.
func parseLoudness(summary []byte, track *Track) error {
	text := string(summary)
	lufs, ok1 := last(integratedPattern, text)
	lra, ok2 := last(rangePattern, text)
	peak, ok3 := last(peakPattern, text)
	if !ok1 || !ok2 || !ok3 {
		return errNoLoudness
	}
	track.LUFS, track.LoudnessRange, track.PeakDB = lufs, lra, peak
	return nil
}

// last returns the number in the last match of pattern in text. "-inf", which ffmpeg writes for silence, is returned as
// -120, a level that no real recording has.
func last(pattern *regexp.Regexp, text string) (float64, bool) {
	matches := pattern.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return 0, false
	}
	raw := matches[len(matches)-1][1]
	if raw == "-inf" {
		return -120, true
	}
	value, err := strconv.ParseFloat(raw, 64)
	return value, err == nil
}

// cleanText makes a tag safe to put in front of a language model and on a page: it keeps printable characters only,
// turns every run of white space into one space, and cuts the result to 80 runes.
func cleanText(s string) string {
	var b strings.Builder
	space := false
	for _, r := range s {
		switch {
		case unicode.IsSpace(r):
			space = true
		case unicode.IsPrint(r):
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			space = false
			b.WriteRune(r)
		}
	}
	out := b.String()
	if runes := []rune(out); len(runes) > maxFieldLength {
		out = strings.TrimSpace(string(runes[:maxFieldLength]))
	}
	return out
}
