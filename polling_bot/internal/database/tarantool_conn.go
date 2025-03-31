package database

import (
	"fmt"
	"time"

	"polling_bot/internal/config"

	"github.com/rs/zerolog"
	"github.com/tarantool/go-tarantool"
)

type TarantoolConnection struct {
	conn *tarantool.Connection
}

func ConnectWithRetry(cfg config.TarantoolConfig, logger zerolog.Logger) (*TarantoolConnection, error) {
	opts := tarantool.Opts{
		User:          cfg.User,
		Pass:          cfg.Password,
		Timeout:       cfg.Timeout,
		Reconnect:     0,              
		MaxReconnects: 0,              
	}

	var conn *tarantool.Connection
	var err error

	logger.Debug().Str("address", cfg.Address).Msg("Connecting to Tarantool")
	
	for attempt := 1; attempt <= cfg.Retries; attempt++ {
		conn, err = tarantool.Connect(cfg.Address, opts)
		if err == nil {
			_, pingErr := conn.Ping()
			if pingErr == nil {
				logger.Info().Msg("Успешное подключение к Tarantool")
				return &TarantoolConnection{conn: conn}, nil
			}
			
			conn.Close()
			err = fmt.Errorf("ping failed: %w", pingErr)
		}

		logger.Error().
			Int("attempt", attempt).
			Int("max_attempts", cfg.Retries).
			Err(err).
			Msg("Ошибка подключения")

		if attempt < cfg.Retries {
			retryDelay := time.Duration(attempt) * time.Second
			logger.Debug().Dur("delay", retryDelay).Msg("Повторная попытка подключения")
			time.Sleep(retryDelay)
		}
	}

	return nil, fmt.Errorf("не удалось подключиться после %d попыток, последняя ошибка: %w", cfg.Retries, err)
}

func (t *TarantoolConnection) Close() error {
	if t.conn != nil {
		return t.conn.Close()
	}
	return nil
}

func (t *TarantoolConnection) Connection() *tarantool.Connection {
	return t.conn
}
