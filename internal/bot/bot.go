package bot

import (
	"context"
	"fmt"
	"log"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/go-github/v69/github"
	"github.com/rusik69/femtoclaw/internal/config"
	"github.com/rusik69/femtoclaw/internal/db"
	openai "github.com/sashabaranov/go-openai"
	"golang.org/x/oauth2"
)

// NewBot initializes a new Bot instance
func NewBot(cfg *config.Config) (*Bot, error) {
	bot, err := tgbotapi.NewBotAPI(cfg.TelegramToken)
	if err != nil {
		return nil, fmt.Errorf("failed to create telegram bot: %w", err)
	}

	bot.Debug = true
	log.Printf("[telegram] authorized on account %s", bot.Self.UserName)

	if _, err := bot.Request(tgbotapi.NewDeleteMyCommands()); err != nil {
		log.Printf("[telegram] delete commands: %v", err)
	}

	config := openai.DefaultConfig(cfg.OpenAIToken)
	if cfg.BaseURL != "" {
		config.BaseURL = cfg.BaseURL
	}
	openaiClient := openai.NewClientWithConfig(config)

	var ghClient *github.Client
	if cfg.GitHubToken != "" {
		ctx := context.Background()
		ts := oauth2.StaticTokenSource(
			&oauth2.Token{AccessToken: cfg.GitHubToken},
		)
		tc := oauth2.NewClient(ctx, ts)
		ghClient = github.NewClient(tc)
	}

	database, err := db.Open(cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open db: %w", err)
	}

	b := &Bot{
		api:          bot,
		openaiClient: openaiClient,
		githubClient: ghClient,
		cfg:          cfg,
		history:      make(map[int64][]openai.ChatCompletionMessage),
		trackedPRs:   make(map[string]*db.TrackedPR),
		db:           database,
		autoWorkCh:   make(map[int64]chan struct{}),
	}
	for _, pr := range mustLoadTrackedPRs(database) {
		key := fmt.Sprintf("%s/%s#%d", pr.Owner, pr.Repo, pr.Number)
		b.trackedPRs[key] = pr
	}
	return b, nil
}

// Start runs the bot loop
func (b *Bot) Start() {
	go b.watchPRComments()

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := b.api.GetUpdatesChan(u)

	for update := range updates {
		if update.Message == nil || update.Message.From == nil {
			continue
		}

		if !b.isAllowed(update.Message.From.UserName) {
			log.Printf("[auth] user %s not allowed", update.Message.From.UserName)
			msg := tgbotapi.NewMessage(update.Message.Chat.ID, "I'm sorry, I generally don't talk to strangers.")
			b.api.Send(msg)
			continue
		}

		log.Printf("[telegram] %s: %s", update.Message.From.UserName, update.Message.Text)

		go b.handleMessage(update.Message)
	}
}

func (b *Bot) isAllowed(username string) bool {
	if len(b.cfg.AllowedUsers) == 0 {
		return true
	}
	for _, u := range b.cfg.AllowedUsers {
		if u == username {
			return true
		}
	}
	return false
}

const helpText = `FemtoClaw — AI coding assistant for GitHub issues.

Commands:
• /help — show this message
• stop — stop auto-work
• auto work / start working / keep working — work on issues until stopped
• work on N issues / fix N issues — work on N issues (max 20)

Or just send any task: "Find and fix a bug in repo X", "Fix issue #42", etc.`

// handleMessage processes a single message using OpenAI with tools
func (b *Bot) handleMessage(msg *tgbotapi.Message) {
	text := strings.TrimSpace(strings.ToLower(msg.Text))
	if text == "/help" || text == "help" {
		reply := tgbotapi.NewMessage(msg.Chat.ID, helpText)
		reply.ReplyToMessageID = msg.MessageID
		b.api.Send(reply)
		return
	}
	if text == "stop" {
		b.stopAutoWork(msg.Chat.ID)
		reply := tgbotapi.NewMessage(msg.Chat.ID, "Stopping...")
		reply.ReplyToMessageID = msg.MessageID
		b.api.Send(reply)
		return
	}
	if parseAutoWork(msg.Text) {
		b.handleBatchWork(msg, 0)
		return
	}
	if count, ok := parseBatchRequest(msg.Text); ok && count > 0 {
		b.handleBatchWork(msg, count)
		return
	}
	result, err := b.processMessage(msg)
	if err != nil {
		reply := tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf("Error: %v", err))
		reply.ReplyToMessageID = msg.MessageID
		b.api.Send(reply)
		return
	}
	if result == "" {
		result = "Done."
	}
	reply := tgbotapi.NewMessage(msg.Chat.ID, result)
	reply.ReplyToMessageID = msg.MessageID
	b.api.Send(reply)
}
