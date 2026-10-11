//go:build live

package ai

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/tmc/langchaingo/llms/ollama"
)

// TestLiveIntroductionsPassTheirChecks asks a real model, on this machine, for introductions and reports how many pass the
// checks of [checkScript]. It is not part of the usual run, because it needs Ollama and the model: run it with
//
//	go test -tags live -run Live -v ./ai
//
// OLLAMA_URL and OLLAMA_MODEL name the server and the model; the defaults are the ones of the server.
func TestLiveIntroductionsPassTheirChecks(t *testing.T) {
	url := os.Getenv("OLLAMA_URL")
	if url == "" {
		url = defaultOllamaURL
	}
	name := os.Getenv("OLLAMA_MODEL")
	if name == "" {
		name = defaultModel
	}
	model, err := ollama.New(ollama.WithModel(name), ollama.WithServerURL(url), ollama.WithFormat("json"))
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	writer := newPresenter(newAsker(model, askOptions{
		Retries:   hostRetries,
		MaxTokens: defaultMaxTokens,
		Logger:    logger,
	}))

	tracks := []Item{
		{
			Track: Track{Name: "02-fifth.wav"},
			Tags: Tags{
				Mood:  moodCalm,
				Genre: "ambient",
			},
		},
		{
			Track: Track{Name: "03-octave.wav"},
			Tags: Tags{
				Mood:  moodRelaxed,
				Genre: "drone",
			},
		},
		{
			Track: Track{Name: "jump-for-the-crown.mp3"},
			Tags: Tags{
				Mood:  moodPlayful,
				Genre: "chiptune",
			},
		},
		{
			Track: Track{
				Name:   "07-blue-hour.flac",
				Artist: "The Lamps",
			},
			Tags: Tags{
				Mood:  moodDreamy,
				Genre: "synthwave",
			},
		},
	}
	passed := 0
	for i, next := range tracks {
		previous := tracks[(i+len(tracks)-1)%len(tracks)]
		started := time.Now()
		script, err := writer.Intro(context.Background(), introRequest{
			Station:  "Go radio",
			Next:     next,
			Previous: &previous,
		})
		if err != nil {
			t.Fatalf("%s: %v", next.Track.Name, err)
		}
		if script.Source == sourceModel {
			passed++
		}
		t.Logf("%-28s %-8s %5.1fs  %s", next.Track.Name, script.Source, time.Since(started).Seconds(), script.Text)
	}
	t.Logf("%d of %d introductions passed the checks", passed, len(tracks))
}
