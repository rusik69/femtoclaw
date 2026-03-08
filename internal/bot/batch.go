package bot

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const maxBatchCount = 20

var batchRegex = regexp.MustCompile(`(?i)(?:work on|fix|solve)\s+(\d+)\s+issues?`)
var autoWorkRegex = regexp.MustCompile(`(?i)^(auto work|start working|keep working)\s*$`)

// parseBatchRequest extracts N from messages like "work on 3 issues", "fix 5 issues", "solve 2 issues".
func parseBatchRequest(text string) (int, bool) {
	if text == "" {
		return 0, false
	}
	m := batchRegex.FindStringSubmatch(text)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n <= 0 {
		return 0, false
	}
	if n > maxBatchCount {
		n = maxBatchCount
	}
	return n, true
}

func parseAutoWork(text string) bool {
	return text != "" && autoWorkRegex.MatchString(strings.TrimSpace(text))
}

func (b *Bot) startAutoWork(chatID int64) chan struct{} {
	b.autoWorkMu.Lock()
	defer b.autoWorkMu.Unlock()
	if ch, ok := b.autoWorkCh[chatID]; ok {
		close(ch)
		delete(b.autoWorkCh, chatID)
	}
	ch := make(chan struct{})
	b.autoWorkCh[chatID] = ch
	return ch
}

func (b *Bot) stopAutoWork(chatID int64) {
	b.autoWorkMu.Lock()
	defer b.autoWorkMu.Unlock()
	if ch, ok := b.autoWorkCh[chatID]; ok {
		close(ch)
		delete(b.autoWorkCh, chatID)
	}
}

func (b *Bot) handleBatchWork(msg *tgbotapi.Message, count int) {
	chatID := msg.Chat.ID

	var stopCh chan struct{}
	if count == 0 {
		stopCh = b.startAutoWork(chatID)
		reply := tgbotapi.NewMessage(chatID, "Starting auto-work. Send 'stop' to stop.")
		reply.ReplyToMessageID = msg.MessageID
		b.api.Send(reply)
	} else {
		reply := tgbotapi.NewMessage(chatID, fmt.Sprintf("Starting batch: %d issues", count))
		reply.ReplyToMessageID = msg.MessageID
		b.api.Send(reply)
	}

	for i := 1; count == 0 || i <= count; i++ {
		if stopCh != nil {
			select {
			case _, ok := <-stopCh:
				if !ok {
					b.api.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("Auto-work stopped after %d issues.", i-1)))
					return
				}
			default:
			}
		}

		b.clearHistory(chatID)

		prompt := "Find a random open issue with no linked PR in any public repo and fix it."
		if i > 1 {
			prompt += " Pick a different repo than previous iterations."
		}

		synthetic := &tgbotapi.Message{
			MessageID: msg.MessageID + i,
			From:      msg.From,
			Chat:      msg.Chat,
			Text:      prompt,
		}

		result, err := b.processMessage(synthetic)
		if err != nil {
			result = fmt.Sprintf("Error: %v", err)
		}
		if result == "" {
			result = "Done."
		}

		if count == 0 {
			b.api.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("Issue %d:\n%s", i, result)))
		} else {
			b.api.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("Issue %d/%d:\n%s", i, count, result)))
		}
	}

	if count > 0 {
		b.api.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("Batch complete: %d issues processed.", count)))
	}
}
