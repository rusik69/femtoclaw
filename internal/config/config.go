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

	githubToken := os.Getenv("GITHUB_TOKEN")
	// GitHub token is optional for basic functionality but required for GitHub tools

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
		AllowedUsers:  allowedUsers,
		WorkDir:       workDir,
	}, nil
}
