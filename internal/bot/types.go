package bot

import (
	"sync"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/go-github/v69/github"
	"github.com/rusik69/femtoclaw/internal/config"
	"github.com/rusik69/femtoclaw/internal/db"
	openai "github.com/sashabaranov/go-openai"
)

// Bot handles the Telegram bot logic
type Bot struct {
	api          *tgbotapi.BotAPI
	openaiClient *openai.Client
	githubClient *github.Client
	cfg          *config.Config
	historyMu    sync.Mutex
	history      map[int64][]openai.ChatCompletionMessage
	trackedPRsMu sync.Mutex
	trackedPRs   map[string]*db.TrackedPR
	db           *db.DB
	autoWorkMu   sync.Mutex
	autoWorkCh   map[int64]chan struct{} // chatID -> stop signal
}

const maxHistoryMessages = 40
