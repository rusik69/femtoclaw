package bot

import (
	openai "github.com/sashabaranov/go-openai"
)

func (b *Bot) getHistory(chatID int64) []openai.ChatCompletionMessage {
	b.historyMu.Lock()
	defer b.historyMu.Unlock()

	history := b.history[chatID]
	if len(history) == 0 {
		return nil
	}

	return fixToolCallSequences(append([]openai.ChatCompletionMessage(nil), history...))
}

func (b *Bot) clearHistory(chatID int64) {
	b.historyMu.Lock()
	defer b.historyMu.Unlock()
	delete(b.history, chatID)
}

// sanitizeMessages ensures no message has empty Content, which some APIs reject as null.
func sanitizeMessages(msgs []openai.ChatCompletionMessage) []openai.ChatCompletionMessage {
	out := make([]openai.ChatCompletionMessage, len(msgs))
	for i, m := range msgs {
		out[i] = m
		if m.Content == "" && len(m.MultiContent) == 0 {
			out[i].Content = " "
		}
	}
	return out
}

// fixToolCallSequences rebuilds the message list keeping only valid sequences.
// An assistant message with tool_calls is kept only when every one of its
// tool_call IDs has a matching tool response immediately following it.
// Only tool responses whose ToolCallID belongs to the assistant are included;
// stray tool messages with unrecognised IDs are dropped.
func fixToolCallSequences(msgs []openai.ChatCompletionMessage) []openai.ChatCompletionMessage {
	var out []openai.ChatCompletionMessage
	i := 0
	for i < len(msgs) {
		m := msgs[i]
		if m.Role == openai.ChatMessageRoleAssistant && len(m.ToolCalls) > 0 {
			expected := make(map[string]bool, len(m.ToolCalls))
			for _, tc := range m.ToolCalls {
				expected[tc.ID] = true
			}
			j := i + 1
			var matched []openai.ChatCompletionMessage
			for j < len(msgs) && msgs[j].Role == openai.ChatMessageRoleTool {
				if expected[msgs[j].ToolCallID] {
					matched = append(matched, msgs[j])
					delete(expected, msgs[j].ToolCallID)
				}
				j++
			}
			if len(expected) == 0 {
				out = append(out, m)
				out = append(out, matched...)
			}
			i = j
		} else if m.Role == openai.ChatMessageRoleTool {
			i++
		} else {
			out = append(out, m)
			i++
		}
	}
	return out
}

// appendHistoryBatch appends multiple messages atomically so that an assistant
// message with tool_calls and all its tool responses are never interleaved with
// messages from concurrently-running tasks.
func (b *Bot) appendHistoryBatch(chatID int64, msgs ...openai.ChatCompletionMessage) {
	if len(msgs) == 0 {
		return
	}
	b.historyMu.Lock()
	defer b.historyMu.Unlock()

	b.history[chatID] = append(b.history[chatID], msgs...)
	if len(b.history[chatID]) > maxHistoryMessages {
		trimmed := b.history[chatID][len(b.history[chatID])-maxHistoryMessages:]
		b.history[chatID] = fixToolCallSequences(trimmed)
	}
}
