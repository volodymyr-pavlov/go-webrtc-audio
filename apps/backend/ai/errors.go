package ai

import (
	"errors"
	"fmt"
)

// errNoProgram is returned by [Exec] when the name of the program is empty.
var errNoProgram = errors.New("run: no program")

// errRejected is returned by [ask] when the model's answer failed the check on every try.
var errRejected = errors.New("ask: the answer of the model was rejected")

// errBadAnswer is the error of the checks in this package for an answer that cannot be used.
var errBadAnswer = errors.New("ask: unusable answer")

// ErrBadOptions is returned by [NewHost] for options that no run could use. The error says which one.
var ErrBadOptions = errors.New("ai: bad options")

// ErrRemoteModel is returned by [NewHost] when the model server is not on this machine and [HostFlagAllowRemoteModel]
// is not set. It wraps [ErrBadOptions].
var ErrRemoteModel = fmt.Errorf("%w: the model server is not on this machine", ErrBadOptions)

// errNoCommand is returned by [newSpeaker] when the command is empty or has no {out} in it.
var errNoCommand = errors.New("voice: the command needs a program and {out} for the file it writes")

// errBadText is returned by [Speaker.Say] for a text that is empty or too long.
var errBadText = errors.New("voice: the text is empty or too long")

// errBadScript is the error of the check of a model's text for the presenter, for a text that cannot be used.
var errBadScript = errors.New("dj: unusable text for the presenter")

// errNoDuration is returned when ffprobe reports no duration for a file, which is the sign of a file that is not audio.
var errNoDuration = errors.New("analyze: file has no duration")

// errBadProbe is the error for output of ffprobe that is not valid JSON.
var errBadProbe = errors.New("analyze: the output of ffprobe is not valid JSON")

// errNoLoudness is returned when the loudness summary of ffmpeg cannot be found in its output.
var errNoLoudness = errors.New("analyze: no loudness in the output of ffmpeg")

// errBadTags is the error of the check of the model's tags, for tags that cannot be used. It is not returned by
// [Tagger.Tag], which falls back instead, but it is what the model is told when its answer is refused.
var errBadTags = errors.New("tag: unusable tags")
