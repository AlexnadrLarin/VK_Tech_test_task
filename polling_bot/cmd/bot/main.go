package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
    "github.com/rs/zerolog"
    "time"

	"polling_bot/internal/bot"
	"polling_bot/internal/config"
)

func main() {
	cfg := config.Load()

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,    
		syscall.SIGTERM, 
		syscall.SIGQUIT, 
	)
	defer stop()

    logger := zerolog.New(
        zerolog.ConsoleWriter{
            Out:        os.Stdout,
            TimeFormat: time.RFC822,
        },
    ).With().Timestamp().Logger()

	bot := bot.NewBot(cfg, logger)

	if err := bot.Start(ctx); err != nil {
		logger.Err(err).Msg("Не удалось запустить бота: %v")
	}
	logger.Info().Msg("Завершение работы бота выполнено")
}
