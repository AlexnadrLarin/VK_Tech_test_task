package config

import (
	"os"
	"time"
)

type Config struct {
	MattermostURL string
	BotToken      string
	HTTPTimeout   time.Duration
}

func Load() Config {
	return Config{
		MattermostURL: os.Getenv("MATTERMOST_URL"),
		BotToken:      os.Getenv("BOT_TOKEN"),
		HTTPTimeout:   10 * time.Second,
	}
}
