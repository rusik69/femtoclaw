package config

import (
	"fmt"
	"os"
	"strings"
)

// Config holds the simplified configuration for the bot
type Config struct {
	TelegramToken string
	OpenAIToken   string
	GitHubToken   string
	GitHubUser    string
	BaseURL       string
	Model         string
	AllowedUsers  []string
	WorkDir       string
}

// LoadConfig loads the configuration from environment variables
func LoadConfig() (*Config, error) {
	telegramToken := os.Getenv("TELEGRAM_API_KEY")
	if telegramToken == "" {
		return nil, fmt.Errorf("TELEGRAM_API_KEY environment variable is required")
	}

	openaiToken := os.Getenv("OPENAI_API_KEY")
	if openaiToken == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY environment variable is required")
	}

	baseURL := os.Getenv("OPENAI_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	model := os.Getenv("OPENAI_MODEL")
	if model == "" {
		model = "gpt-4-turbo-preview"
	}

	githubToken := os.Getenv("GITHUB_TOKEN")
	githubUser := os.Getenv("GITHUB_USER")

	allowedUsersStr := os.Getenv("ALLOWED_USERS")
	var allowedUsers []string
	if allowedUsersStr != "" {
		allowedUsers = strings.Split(allowedUsersStr, ",")
		for i := range allowedUsers {
			allowedUsers[i] = strings.TrimSpace(allowedUsers[i])
		}
	}

	workDir := os.Getenv("WORKDIR")
	if workDir == "" {
		workDir = "."
	}

	return &Config{
		TelegramToken: telegramToken,
		OpenAIToken:   openaiToken,
		GitHubToken:   githubToken,
		GitHubUser:    githubUser,
		BaseURL:       baseURL,
		Model:         model,
		AllowedUsers:  allowedUsers,
		WorkDir:       workDir,
	}, nil
}
