package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"polling_bot/internal/bot"
	"polling_bot/internal/config"
	"polling_bot/internal/database"
	"polling_bot/internal/handler"
	"polling_bot/internal/repository"
	"polling_bot/internal/service"

	"github.com/rs/zerolog"
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

    tarantoolCfg := config.TarantoolConfigLoad()

    conn, err := database.ConnectWithRetry(tarantoolCfg, logger)
    if err != nil {
        logger.Err(err).Msg("Не подключиться к Tarantool: %v")
        return
    }
    defer conn.Close()
    
    repo := repository.NewTarantoolPollRepo(conn.Connection(), tarantoolCfg.Database)

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
