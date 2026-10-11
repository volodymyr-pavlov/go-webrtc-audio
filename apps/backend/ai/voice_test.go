package ai

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

type call struct {
	stdin string
	name  string
	args  []string
}

type recorder struct {
	calls []call
	err   error
}

func (r *recorder) Run(_ context.Context, stdin io.Reader, _, _ io.Writer, name string, args ...string) error {
	var text []byte
	if stdin != nil {
		text, _ = io.ReadAll(stdin)
	}
	r.calls = append(r.calls, call{
		stdin: string(text),
		name:  name,
		args:  args,
	})
	return r.err
}

func TestSayStartsTheSpeechProgramWithTheTextOnStdinAndThenFFmpeg(t *testing.T) {
	r := &recorder{}
	s, err := newSpeaker(r.Run, voiceOptions{
		Command:   []string{"say", "-f", "-", "-o", "{out}"},
		Extension: ".aiff",
	})
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "clip.wav")
	if err := s.Say(context.Background(), "Welcome to Night Owls.", out); err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 2 {
		t.Fatalf("%d calls, want the speech program and ffmpeg", len(r.calls))
	}
	speech, convert := r.calls[0], r.calls[1]
	if speech.name != "say" || speech.stdin != "Welcome to Night Owls." {
		t.Fatalf("speech: %+v", speech)
	}
	raw := speech.args[len(speech.args)-1]
	if !strings.HasSuffix(raw, "speech.aiff") || strings.Contains(raw, "{out}") {
		t.Fatalf("the output file was not filled in: %v", speech.args)
	}
	for _, arg := range speech.args {
		if strings.Contains(arg, "Welcome") {
			t.Fatalf("the text is in an argument: %v", speech.args)
		}
	}
	if convert.name != "ffmpeg" || convert.stdin != "" || convert.args[len(convert.args)-1] != out {
		t.Fatalf("convert: %+v", convert)
	}
	joined := strings.Join(convert.args, " ")
	for _, want := range []string{"-i " + raw, "loudnorm=I=-16:TP=-1.5:LRA=11", "-ar 48000", "-ac 2"} {
		if !strings.Contains(joined, want) {
			t.Errorf("ffmpeg arguments lack %q: %s", want, joined)
		}
	}
}

func TestTheTextNeverBecomesAnArgumentWhateverItSays(t *testing.T) {
	r := &recorder{}
	s, _ := newSpeaker(r.Run, voiceOptions{Command: []string{"piper", "--output_file", "{out}"}})
	text := "; rm -rf ~ $(whoami) `id` --output_file /etc/passwd"
	if err := s.Say(context.Background(), text, "out.wav"); err != nil {
		t.Fatal(err)
	}
	for _, c := range r.calls {
		for _, arg := range c.args {
			if strings.Contains(arg, "rm -rf") || arg == "/etc/passwd" {
				t.Fatalf("the text reached the arguments of %s: %v", c.name, c.args)
			}
		}
	}
	if r.calls[0].stdin != text {
		t.Fatalf("stdin %q", r.calls[0].stdin)
	}
}

func TestNewRefusesACommandThatCannotWriteAClip(t *testing.T) {
	for _, command := range [][]string{nil, {}, {""}, {"say"}, {"say", "-f", "-"}, {"{out}", "x"}} {
		if _, err := newSpeaker((&recorder{}).Run, voiceOptions{Command: command}); !errors.Is(err, errNoCommand) {
			t.Errorf("%v: got %v, want errNoCommand", command, err)
		}
	}
}

func TestSayRefusesATextThatIsEmptyOrLong(t *testing.T) {
	s, _ := newSpeaker((&recorder{}).Run, voiceOptions{Command: []string{"piper", "-f", "{out}"}})
	for _, text := range []string{"", "  \n", strings.Repeat("a", maxTextBytes+1)} {
		if err := s.Say(context.Background(), text, "out.wav"); !errors.Is(err, errBadText) {
			t.Errorf("%d bytes: got %v, want errBadText", len(text), err)
		}
	}
}

func TestSayReportsAProgramThatFailsAndDoesNotRunFFmpegAfterIt(t *testing.T) {
	boom := errors.New("no voice installed")
	r := &recorder{err: boom}
	s, _ := newSpeaker(r.Run, voiceOptions{Command: []string{"piper", "-f", "{out}"}})
	if err := s.Say(context.Background(), "Hello.", "out.wav"); !errors.Is(err, boom) {
		t.Fatalf("got %v", err)
	}
	if len(r.calls) != 1 {
		t.Fatalf("%d calls after a failed speech program, want 1", len(r.calls))
	}
}
