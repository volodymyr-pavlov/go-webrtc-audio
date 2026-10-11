package ai

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/tmc/langchaingo/llms"
)

var prompt = Prompt{
	Template: "Say a word. {{.feedback}} Topic: {{.topic}}",
	Vars:     []string{"topic"},
}

func word(answer string) (string, error) {
	if answer != "ok" {
		return "", errors.New("say ok")
	}
	return answer, nil
}

func TestDoReturnsTheFirstAnswerThatPasses(t *testing.T) {
	model := &fakeModel{Answers: []string{"ok"}}
	got, err := ask(context.Background(), newAsker(model, askOptions{}), askRequest[string]{
		Prompt:      prompt,
		Vars:        map[string]any{"topic": "rain"},
		Temperature: 0.2,
		Check:       word,
	})
	if err != nil || got != "ok" {
		t.Fatalf("got %q, %v", got, err)
	}
	if len(model.prompts()) != 1 || !strings.Contains(model.prompts()[0], "Topic: rain") {
		t.Fatalf("prompts: %q", model.prompts())
	}
}

func TestDoAsksAgainWithTheReasonOfTheRejection(t *testing.T) {
	model := &fakeModel{Answers: []string{"nope", "ok"}}
	got, err := ask(context.Background(), newAsker(model, askOptions{}), askRequest[string]{
		Prompt:      prompt,
		Vars:        map[string]any{"topic": "rain"},
		Temperature: 0.2,
		Check:       word,
	})
	if err != nil || got != "ok" {
		t.Fatalf("got %q, %v", got, err)
	}
	if len(model.prompts()) != 2 {
		t.Fatalf("%d prompts, want 2", len(model.prompts()))
	}
	if strings.Contains(model.prompts()[0], "rejected") {
		t.Fatalf("the first prompt has feedback: %q", model.prompts()[0])
	}
	if !strings.Contains(model.prompts()[1], "rejected: say ok") {
		t.Fatalf("the second prompt does not say why: %q", model.prompts()[1])
	}
}

func TestDoGivesUpWithErrRejectedAfterTheRetries(t *testing.T) {
	model := &fakeModel{Answers: []string{"nope"}}
	_, err := ask(context.Background(), newAsker(model, askOptions{Retries: 1}), askRequest[string]{
		Prompt:      prompt,
		Vars:        map[string]any{"topic": "x"},
		Temperature: 0.2,
		Check:       word,
	})
	if !errors.Is(err, errRejected) {
		t.Fatalf("got %v, want errRejected", err)
	}
	if len(model.prompts()) != 2 {
		t.Fatalf("%d tries, want 2 (one and one retry)", len(model.prompts()))
	}
}

func TestDoWithANegativeRetryCountAsksOnce(t *testing.T) {
	model := &fakeModel{Answers: []string{"nope"}}
	_, err := ask(context.Background(), newAsker(model, askOptions{Retries: -1}), askRequest[string]{
		Prompt:      prompt,
		Vars:        map[string]any{"topic": "x"},
		Temperature: 0.2,
		Check:       word,
	})
	if !errors.Is(err, errRejected) || len(model.prompts()) != 1 {
		t.Fatalf("got %v after %d tries", err, len(model.prompts()))
	}
}

func TestDoDoesNotRetryAModelThatFailsAndDoesNotCallItRejected(t *testing.T) {
	boom := errors.New("connection refused")
	model := &fakeModel{Err: boom}
	_, err := ask(context.Background(), newAsker(model, askOptions{}), askRequest[string]{
		Prompt:      prompt,
		Vars:        map[string]any{"topic": "x"},
		Temperature: 0.2,
		Check:       word,
	})
	if !errors.Is(err, boom) || errors.Is(err, errRejected) {
		t.Fatalf("got %v, want the error of the model and not errRejected", err)
	}
}

func TestDoRefusesAnAnswerThatIsTooLongBeforeCheckingIt(t *testing.T) {
	model := &fakeModel{Answers: []string{strings.Repeat("a", maxAnswerLength+1)}}
	checked := false
	_, err := ask(context.Background(), newAsker(model, askOptions{Retries: -1}), askRequest[string]{
		Prompt:      prompt,
		Vars:        map[string]any{"topic": "x"},
		Temperature: 0.2,
		Check: func(string) (string, error) {
			checked = true
			return "", nil
		},
	})
	if !errors.Is(err, errRejected) || checked {
		t.Fatalf("got %v, checked %v", err, checked)
	}
}

func TestUntrustedValuesAreDataAndNotTemplateText(t *testing.T) {
	model := &fakeModel{Answers: []string{"ok"}}
	hostile := "{{ .feedback }} {{ range $i := until 1000 }}x{{ end }}"
	if _, err := ask(context.Background(), newAsker(model, askOptions{}), askRequest[string]{
		Prompt:      prompt,
		Vars:        map[string]any{"topic": hostile},
		Temperature: 0.2,
		Check:       word,
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(model.prompts()[0], hostile) || strings.Contains(model.prompts()[0], "xxxxxxxx") {
		t.Fatalf("a value was run as a template: %q", model.prompts()[0])
	}
}

func TestJSONObjectTakesTheObjectOutOfProseAndFences(t *testing.T) {
	answer := "Sure! Here you go:\n```json\n{\"mood\": \"calm\", \"energy\": 2, \"extra\": true}\n```\nHope it helps."

	got, err := jsonObject(answer)

	if err != nil {
		t.Fatal(err)
	}
	if got.Get("mood").String() != "calm" || got.Get("energy").Int() != 2 {
		t.Fatalf("%s", got.Raw)
	}
}

func TestJSONObjectRefusesWhatIsNotOneObject(t *testing.T) {
	for _, answer := range []string{"", "no json here", "} {", `{"a": }`, `{"a": 1} {"a": 2}`} {
		if _, err := jsonObject(answer); !errors.Is(err, errBadAnswer) {
			t.Errorf("%q: got %v, want errBadAnswer", answer, err)
		}
	}
}

func TestLineAcceptsPlainWordsAndTidiesTheSpaces(t *testing.T) {
	got, err := plainLine("  Up next:\n a little   night music — enjoy! ", 80)
	if err != nil || got != "Up next: a little night music — enjoy!" {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := plainLine("Mixed: Café, “quotes” and 3 numbers (50%).", 80); err != nil {
		t.Fatal(err)
	}
}

func TestLineRefusesWhatAProgramOrMarkupCouldUse(t *testing.T) {
	for _, text := range []string{
		"", "   ",
		"see https://evil.example/x", "visit www.evil.example", "ftp://x",
		"<script>alert(1)</script>", "../../etc", "a/b", "{{ .secret }}", "`rm -rf ~`", "a\\nb", "a | b", "x = y", "mail me@evil.example",
		"# heading", "**bold**", "snake_case", "bell\x07", "emoji 🎵",
		strings.Repeat("a", 81),
	} {
		if _, err := plainLine(text, 80); !errors.Is(err, errBadAnswer) {
			t.Errorf("%q: got %v, want errBadAnswer", text, err)
		}
	}
}

func TestWordsCountsWords(t *testing.T) {
	if got := wordCount("  one two\tthree\nfour "); got != 4 {
		t.Fatalf("got %d, want 4", got)
	}
}

// hanging is a model that never answers until its context ends.
type hanging struct{}

var _ llms.Model = hanging{}

func (hanging) GenerateContent(ctx context.Context, _ []llms.MessageContent, _ ...llms.CallOption) (*llms.ContentResponse, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (h hanging) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	return llms.GenerateFromSinglePrompt(ctx, h, prompt, options...)
}

func TestDoGivesUpOnAModelThatHangsAfterTheTimeout(t *testing.T) {
	started := time.Now()
	_, err := ask(context.Background(), newAsker(hanging{}, askOptions{Timeout: 50 * time.Millisecond}), askRequest[string]{
		Prompt:      prompt,
		Vars:        map[string]any{"topic": "x"},
		Temperature: 0.2,
		Check:       word,
	})
	if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, errRejected) {
		t.Fatalf("got %v, want a deadline error that is not errRejected", err)
	}
	if time.Since(started) > 5*time.Second {
		t.Fatalf("it took %v", time.Since(started))
	}
}
