package bot

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/rusik69/femtoclaw/internal/db"
	"github.com/rusik69/femtoclaw/internal/tools"
	openai "github.com/sashabaranov/go-openai"
)

func mustLoadTrackedPRs(database *db.DB) []*db.TrackedPR {
	prs, err := database.ListTrackedPRs()
	if err != nil {
		log.Printf("[db] load tracked PRs: %v", err)
		return nil
	}
	return prs
}

func (b *Bot) watchPRComments() {
	if b.githubClient == nil || b.cfg.GitHubUser == "" {
		log.Printf("[pr-watcher] disabled: no github client or GITHUB_USER")
		return
	}
	interval := b.cfg.PRPollInterval
	if interval <= 0 {
		interval = 60 * time.Second
	}
	log.Printf("[pr-watcher] started, polling every %v", interval)
	for {
		time.Sleep(interval)
		prs := b.getTrackedPRs()
		if len(prs) == 0 {
			continue
		}
		ctx := context.Background()
		for _, pr := range prs {
			comments, err := tools.GithubListPRComments(b.githubClient, pr.Owner, pr.Repo, pr.Number, pr.LastChecked)
			if err != nil {
				log.Printf("[pr-watcher] %s/%s#%d list comments: %v", pr.Owner, pr.Repo, pr.Number, err)
				continue
			}
			now := time.Now()
			b.updateTrackedPRLastChecked(pr.Owner, pr.Repo, pr.Number, now)
			for _, c := range comments {
				if c.User == nil || c.Body == nil {
					continue
				}
				author := ""
				if c.User.Login != nil {
					author = *c.User.Login
				}
				if author == b.cfg.GitHubUser {
					continue
				}
				body := *c.Body
				log.Printf("[pr-watcher] %s/%s#%d new comment by %s", pr.Owner, pr.Repo, pr.Number, author)
				sysPrompt := fmt.Sprintf(`You are FemtoClaw, an AI coding assistant. A reviewer commented on your PR "%s". Reply concisely and helpfully. Keep it short.`, pr.Title)
				msgs := []openai.ChatCompletionMessage{
					{Role: openai.ChatMessageRoleSystem, Content: sysPrompt},
					{Role: openai.ChatMessageRoleUser, Content: body},
				}
				resp, err := b.openaiClient.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
					Model:    b.cfg.Model,
					Messages: msgs,
				})
				if err != nil {
					log.Printf("[pr-watcher] %s/%s#%d openai: %v", pr.Owner, pr.Repo, pr.Number, err)
					continue
				}
				if len(resp.Choices) == 0 {
					continue
				}
				reply := strings.TrimSpace(resp.Choices[0].Message.Content)
				if reply == "" {
					continue
				}
				_, err = tools.GithubCommentIssue(b.githubClient, pr.Owner, pr.Repo, pr.Number, reply)
				if err != nil {
					log.Printf("[pr-watcher] %s/%s#%d post reply: %v", pr.Owner, pr.Repo, pr.Number, err)
					continue
				}
				log.Printf("[pr-watcher] %s/%s#%d replied to %s", pr.Owner, pr.Repo, pr.Number, author)

				if pr.ChatID != 0 {
					prURL := fmt.Sprintf("https://github.com/%s/%s/pull/%d", pr.Owner, pr.Repo, pr.Number)
					notify := tgbotapi.NewMessage(pr.ChatID,
						fmt.Sprintf("Replied to @%s on PR %s\n%s", author, prURL, pr.Title))
					if _, err := b.api.Send(notify); err != nil {
						log.Printf("[pr-watcher] notify telegram: %v", err)
					}
				}
			}
		}
	}
}

func (b *Bot) registerTrackedPR(owner, repo string, number int, title string, chatID int64) {
	key := fmt.Sprintf("%s/%s#%d", owner, repo, number)
	pr := &db.TrackedPR{
		Owner:       owner,
		Repo:        repo,
		Number:      number,
		Title:       title,
		LastChecked: time.Now(),
		ChatID:      chatID,
	}
	b.trackedPRsMu.Lock()
	b.trackedPRs[key] = pr
	b.trackedPRsMu.Unlock()
	if b.db != nil {
		_ = b.db.SaveTrackedPR(key, pr)
	}
}

func (b *Bot) getTrackedPRs() []*db.TrackedPR {
	b.trackedPRsMu.Lock()
	defer b.trackedPRsMu.Unlock()
	out := make([]*db.TrackedPR, 0, len(b.trackedPRs))
	for _, pr := range b.trackedPRs {
		out = append(out, pr)
	}
	return out
}

func (b *Bot) updateTrackedPRLastChecked(owner, repo string, number int, t time.Time) {
	key := fmt.Sprintf("%s/%s#%d", owner, repo, number)
	b.trackedPRsMu.Lock()
	if pr, ok := b.trackedPRs[key]; ok {
		pr.LastChecked = t
	}
	b.trackedPRsMu.Unlock()
	if b.db != nil {
		_ = b.db.UpdateLastChecked(key, t)
	}
}

func parsePRURL(output string) (owner, repo string, number int, ok bool) {
	const prefix = "PR created: https://github.com/"
	if !strings.HasPrefix(output, prefix) {
		return "", "", 0, false
	}
	path := strings.TrimPrefix(output, prefix)
	parts := strings.Split(path, "/")
	if len(parts) < 5 || parts[3] != "pull" {
		return "", "", 0, false
	}
	var n int
	if _, err := fmt.Sscanf(parts[4], "%d", &n); err != nil {
		return "", "", 0, false
	}
	return parts[0], parts[1], n, true
}
