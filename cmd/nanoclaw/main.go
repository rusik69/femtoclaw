package main

import (
	"log"

	"github.com/rusik69/nanoclaw/internal/bot"
	"github.com/rusik69/nanoclaw/internal/config"
)

func main() {
	// Load configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatal(err)
	}

	// Initialize bot
	b, err := bot.NewBot(cfg)
	if err != nil {
		log.Fatal(err)
	}

	// Start bot
	b.Start()
}
