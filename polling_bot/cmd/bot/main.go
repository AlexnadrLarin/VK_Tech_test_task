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
    "polling_bot/internal/handler"
    "polling_bot/internal/service"
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

    repo := service.NewInMemoryPollStorage()

    service := service.NewPollService(repo)

    handler := handler.NewPollCommandHandler(service)

	bot, err := bot.NewBot(cfg, logger, handler)
    if  err != nil {
		logger.Err(err).Msg("Не удалось создать бота: %v")
        return 
	}

	if err := bot.Start(ctx); err != nil {
		logger.Err(err).Msg("Не удалось запустить бота: %v")
	}
	logger.Info().Msg("Завершение работы бота выполнено")
}
