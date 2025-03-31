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

type TarantoolConfig struct {
	Address  string
	User     string
	Password string
	Database string
	Retries  int
	Timeout  time.Duration
}

func Load() Config {
	return Config{
		MattermostURL: os.Getenv("MATTERMOST_URL"),
		BotToken:      os.Getenv("BOT_TOKEN"),
		HTTPTimeout:   10 * time.Second,
	}
}

func TarantoolConfigLoad() TarantoolConfig {
	return TarantoolConfig{
		Address:  os.Getenv("TARANTOOL_ADDR"),
		User:     os.Getenv("TARANTOOL_USER"),
		Password: os.Getenv("TARANTOOL_PASSWORD"),
		Database: os.Getenv("TARANTOOL_DATABASE"),
		Retries:  5,
		Timeout:  5 * time.Second,
	}
}