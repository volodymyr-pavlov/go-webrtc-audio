package ai

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode"

	"github.com/tidwall/gjson"
	"github.com/tmc/langchaingo/chains"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/prompts"
)

const (
	// defaultRetries is how many more times a rejected answer is asked for.
	defaultRetries = 2

	// askTimeout is the time one call to the model may take.
	askTimeout = 2 * time.Minute

	// maxAnswerLength is the longest answer read from a model, in bytes. A longer one is refused before it is parsed.
	maxAnswerLength = 16 << 10
)

// askOptions describe an [Asker].
type askOptions struct {
	// Retries is how many more times a rejected answer is asked for. The default is 2; a negative value asks once.
	Retries int

	// Seed makes the model's choices repeatable where the model supports it. Zero leaves it to the model.
	Seed int

	// Timeout is how long one call to the model may take. The default is two minutes, which a small model on a CPU needs
	// for a long answer and a model that has hung does not get past.
	Timeout time.Duration

	// Logger receives, at the debug level, every answer that fails its check, with the reason, so that a model that
	// keeps failing can be understood. The default discards them.
	Logger *slog.Logger

	// MaxTokens is the longest answer to ask for, in tokens. Zero leaves it to the model.
	MaxTokens int
}

// An Asker runs prompts through one language model.
type Asker struct {
	model     llms.Model
	retries   int
	seed      int
	maxTokens int
	timeout   time.Duration
	logger    *slog.Logger
}

// newAsker returns an [Asker] for model.
func newAsker(model llms.Model, opts askOptions) *Asker {
	a := &Asker{
		model:     model,
		retries:   opts.Retries,
		seed:      opts.Seed,
		maxTokens: opts.MaxTokens,
		timeout:   opts.Timeout,
		logger:    opts.Logger,
	}
	if a.logger == nil {
		a.logger = slog.New(slog.DiscardHandler)
	}
	if a.timeout <= 0 {
		a.timeout = askTimeout
	}
	if a.retries == 0 {
		a.retries = defaultRetries
	}
	if a.retries < 0 {
		a.retries = 0
	}
	return a
}

// A Prompt is a LangChain prompt template. It may use the variables in Vars, and it must use "feedback", which holds an
// empty text on the first try and, after an answer was rejected, the reason, so that the model can correct itself.
type Prompt struct {
	Template string
	Vars     []string
}

// An askRequest is one question for [ask].
type askRequest[T any] struct {
	// Prompt is the template to run.
	Prompt Prompt

	// Vars holds the values of the variables of Prompt, except "feedback".
	Vars map[string]any

	// Temperature says how much the model may vary its answer.
	Temperature float64

	// Check reads an answer and returns its value, or an error if the answer cannot be used.
	Check func(answer string) (T, error)
}

// ask runs the prompt of req and passes the answer to its Check. If Check returns an error the model is asked again,
// up to the retries of the [Asker], with the error in the variable "feedback". It returns the value of the first
// answer that passes. It returns an error that matches [errRejected] if none does, and the error of the model at once
// if the model itself fails.
func ask[T any](ctx context.Context, a *Asker, req askRequest[T]) (T, error) {
	var zero T
	chain := chains.NewLLMChain(a.model, prompts.NewPromptTemplate(req.Prompt.Template,
		append([]string{"feedback"}, req.Prompt.Vars...)))
	options := a.callOptions(req.Temperature)

	values := make(map[string]any, len(req.Vars)+1)
	for k, v := range req.Vars {
		values[k] = v
	}
	values["feedback"] = ""
	var lastErr error
	for attempt := 0; attempt <= a.retries; attempt++ {
		attemptCtx, cancel := context.WithTimeout(ctx, a.timeout)
		answer, err := chains.Predict(attemptCtx, chain, values, options...)
		cancel()
		if err != nil {
			return zero, fmt.Errorf("ask: the model failed: %w", err)
		}
		if len(answer) > maxAnswerLength {
			lastErr = fmt.Errorf("%w: the answer is longer than %d bytes", errBadAnswer, maxAnswerLength)
			values["feedback"] = rejection(lastErr)
			continue
		}
		value, err := req.Check(answer)
		if err == nil {
			return value, nil
		}
		lastErr = err
		a.logger.Debug("answer rejected", "attempt", attempt+1, "reason", err, "answer", shorten(answer, 300))
		values["feedback"] = rejection(err)
	}
	return zero, fmt.Errorf("%w: %w", errRejected, lastErr)
}

// rejection returns the text that tells the model why its answer was rejected.
func rejection(err error) string {
	return "Your previous answer was rejected: " + err.Error() + ". Answer again and follow the rules exactly."
}

// callOptions returns the options of one call to the model: the temperature, and the seed and the token limit if the
// [Asker] has them.
func (a *Asker) callOptions(temperature float64) []chains.ChainCallOption {
	options := []chains.ChainCallOption{chains.WithTemperature(temperature)}
	if a.seed != 0 {
		options = append(options, chains.WithSeed(a.seed))
	}
	if a.maxTokens > 0 {
		options = append(options, chains.WithMaxTokens(a.maxTokens))
	}
	return options
}

// jsonObject returns the one JSON object in answer. Models wrap JSON in prose or in a fenced block, so the text from
// the first "{" to the last "}" is taken; whatever is around it is dropped. A field that the caller reads and that is
// not there gives a result that does not exist, so the caller must check every field it uses.
//
// It returns [errBadAnswer] if there is no object, or if the text is not valid JSON, which includes two objects in a
// row.
func jsonObject(answer string) (gjson.Result, error) {
	start, end := strings.IndexByte(answer, '{'), strings.LastIndexByte(answer, '}')
	if start < 0 || end < start {
		return gjson.Result{}, fmt.Errorf("%w: there is no JSON object", errBadAnswer)
	}
	text := answer[start : end+1]
	if !gjson.Valid(text) {
		return gjson.Result{}, fmt.Errorf("%w: the JSON is not valid, or has more than one value", errBadAnswer)
	}
	return gjson.Parse(text), nil
}

// plainLine checks that text is one short line of plain words and returns it with its white space made single spaces.
// It allows letters, digits, spaces and the punctuation of ordinary prose, and refuses everything that a program or a
// markup language could use: angle brackets, braces, backticks, slashes and backslashes, pipes, equals signs, at signs,
// hashes, stars, underscores, links and control characters. It refuses an empty text and one longer than maxRunes
// runes.
func plainLine(text string, maxRunes int) (string, error) {
	fields := strings.Fields(text)
	line := strings.Join(fields, " ")
	if line == "" {
		return "", fmt.Errorf("%w: the text is empty", errBadAnswer)
	}
	if n := len([]rune(line)); n > maxRunes {
		return "", fmt.Errorf("%w: the text has %d characters and may have %d", errBadAnswer, n, maxRunes)
	}
	lower := strings.ToLower(line)
	if strings.Contains(lower, "://") || strings.Contains(lower, "www.") {
		return "", fmt.Errorf("%w: the text has a link", errBadAnswer)
	}
	for _, r := range line {
		if !isPlain(r) {
			return "", fmt.Errorf("%w: the text has the character %q, which plain words do not need", errBadAnswer, r)
		}
	}
	return line, nil
}

// isPlain reports whether r may appear in a line of plain words.
func isPlain(r rune) bool {
	switch {
	case unicode.IsLetter(r), unicode.IsDigit(r), r == ' ':
		return true
	}
	return strings.ContainsRune(".,;:!?'’\"“”‘-–—()&%+", r)
}

// wordCount returns the number of words in text.
func wordCount(text string) int {
	return len(strings.Fields(text))
}

// shorten returns text cut to at most n bytes, with an ellipsis if it was cut, for a log line.
func shorten(text string, n int) string {
	if len(text) <= n {
		return text
	}
	return text[:n] + "..."
}
