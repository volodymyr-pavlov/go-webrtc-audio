package ai

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	// placeholder is the word in the command line that stands for the file the speech program writes.
	placeholder = "{out}"

	// clipTimeout is how long one clip may take to make.
	clipTimeout = 2 * time.Minute

	// maxTextBytes is the longest script that is spoken. A script is a sentence or two.
	maxTextBytes = 1 << 10
)

// voiceOptions describe a [Speaker].
type voiceOptions struct {
	// Command is the speech program and its arguments, one element each. {out} in an argument stands for the file to
	// write. At least one argument must contain it.
	Command []string

	// Extension is the extension of the file the speech program writes, with its dot, such as ".wav" or ".aiff". It tells
	// ffmpeg what to read. The default is ".wav".
	Extension string

	// FFmpeg is the program that converts the clip. The default is "ffmpeg".
	FFmpeg string

	// LUFS is the loudness the clips are brought to. The default is -16, a little louder than the -23 of broadcast
	// standards, because the clips play between recorded tracks that are mostly louder.
	LUFS float64

	// Logger is where a failure to remove the clip's directory is written. The default discards it.
	Logger *slog.Logger
}

// A Speaker makes audio clips from text.
type Speaker struct {
	runner    Runner
	command   []string
	extension string
	ffmpeg    string
	lufs      float64
	logger    *slog.Logger
}

// newSpeaker returns a [Speaker] that starts its programs with runner. It returns [errNoCommand] if the command is not
// usable.
func newSpeaker(runner Runner, opts voiceOptions) (*Speaker, error) {
	if len(opts.Command) == 0 || opts.Command[0] == "" || strings.Contains(opts.Command[0], placeholder) {
		return nil, errNoCommand
	}
	hasOut := false
	for _, arg := range opts.Command[1:] {
		hasOut = hasOut || strings.Contains(arg, placeholder)
	}
	if !hasOut {
		return nil, errNoCommand
	}
	s := &Speaker{
		runner:    runner,
		command:   append([]string(nil), opts.Command...),
		extension: opts.Extension,
		ffmpeg:    opts.FFmpeg,
		lufs:      opts.LUFS,
		logger:    opts.Logger,
	}
	if s.extension == "" {
		s.extension = defaultTTSExt
	}
	if s.ffmpeg == "" {
		s.ffmpeg = defaultFFmpeg
	}
	if s.logger == nil {
		s.logger = slog.New(slog.DiscardHandler)
	}
	if s.lufs == 0 {
		s.lufs = -16
	}
	return s, nil
}

// Say speaks text and writes the clip to path as 48 kHz stereo WAV, at the loudness of the speaker. It returns
// [errBadText] for a text that is empty or longer than a kilobyte.
func (s *Speaker) Say(ctx context.Context, text, path string) error {
	if strings.TrimSpace(text) == "" || len(text) > maxTextBytes {
		return errBadText
	}
	ctx, cancel := context.WithTimeout(ctx, clipTimeout)
	defer cancel()

	dir, err := os.MkdirTemp("", "voice-")
	if err != nil {
		return fmt.Errorf("voice: make a directory for the clip: %w", err)
	}
	defer func() {
		if err := os.RemoveAll(dir); err != nil {
			s.logger.Debug("the clip's folder was not removed", "err", err)
		}
	}()
	raw := filepath.Join(dir, "speech"+s.extension)

	args := make([]string, len(s.command)-1)
	for i, arg := range s.command[1:] {
		args[i] = strings.ReplaceAll(arg, placeholder, raw)
	}
	if err := s.runner(ctx, strings.NewReader(text), nil, nil, s.command[0], args...); err != nil {
		return fmt.Errorf("voice: speak: %w", err)
	}
	if err := s.runner(ctx, nil, nil, nil, s.ffmpeg,
		"-y", "-v", "error", "-i", raw,
		"-af", "loudnorm=I="+strconv.FormatFloat(s.lufs, 'f', -1, 64)+":TP=-1.5:LRA=11",
		"-ar", "48000", "-ac", "2", path,
	); err != nil {
		return fmt.Errorf("voice: convert the clip: %w", err)
	}
	return nil
}
