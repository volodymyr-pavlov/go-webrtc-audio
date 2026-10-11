package ai

import (
	"context"
	"strings"
	"sync/atomic"

	"github.com/tmc/langchaingo/llms"

	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/inmem"
)

var _ llms.Model = &fakeModel{}

// A fakeModel answers the n-th question with Answers[n], and every later one with the last answer. It records each prompt.
// If Err is set, it fails every call with it. It is safe to use from several goroutines.
type fakeModel struct {
	Answers []string
	Err     error

	calls atomic.Int64
	log   inmem.List[string]
}

// prompts returns the prompts recorded so far.
func (m *fakeModel) prompts() []string {
	return m.log.All()
}

// GenerateContent implements [llms.Model].
func (m *fakeModel) GenerateContent(
	_ context.Context, messages []llms.MessageContent, _ ...llms.CallOption,
) (*llms.ContentResponse, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	var prompt strings.Builder
	for _, message := range messages {
		for _, part := range message.Parts {
			if text, ok := part.(llms.TextContent); ok {
				prompt.WriteString(text.Text)
			}
		}
	}
	n := int(m.calls.Add(1)) - 1
	m.log.Add(prompt.String())
	answer := m.Answers[min(n, len(m.Answers)-1)]
	return &llms.ContentResponse{Choices: []*llms.ContentChoice{{Content: answer}}}, nil
}

// Call implements [llms.Model].
func (m *fakeModel) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	return llms.GenerateFromSinglePrompt(ctx, m, prompt, options...)
}
