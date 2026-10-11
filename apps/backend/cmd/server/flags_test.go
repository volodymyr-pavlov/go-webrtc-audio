package main

import (
	"slices"
	"testing"

	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/config"
)

func TestPresenterFlagsHaveDefaultsThatAreOffAndLocal(t *testing.T) {
	opts, err := parseFlags(nil)
	if err != nil {
		t.Fatal(err)
	}
	if opts.Flags&(config.FlagPresenter|config.FlagRemoteModel) != 0 || opts.OllamaURL != "http://127.0.0.1:11434" ||
		opts.PresenterModel != "phi3.5" || len(opts.PresenterTTS) != 0 {
		t.Fatalf("%+v", opts)
	}
}

func TestPresenterFlagsSplitTheSpeechCommandOnSpaces(t *testing.T) {
	opts, err := parseFlags([]string{
		"-presenter", "-presenter-tts", "piper --model voice.onnx --output_file {out}", "-presenter-name", "Night Owls",
	})
	if err != nil {
		t.Fatal(err)
	}
	if opts.Flags&config.FlagPresenter == 0 || opts.PresenterName != "Night Owls" ||
		!slices.Equal(opts.PresenterTTS, []string{"piper", "--model", "voice.onnx", "--output_file", "{out}"}) {
		t.Fatalf("%+v", opts)
	}
}

func TestTheDefaultFlagsPassTheValidationOfTheSettings(t *testing.T) {
	opts, err := parseFlags(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := opts.Validate(); err != nil {
		t.Fatalf("the defaults break a rule: %v", err)
	}
}
