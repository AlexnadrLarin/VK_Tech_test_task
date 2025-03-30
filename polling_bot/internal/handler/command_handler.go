package handler

import (
	"context"
	"strings"
	"unicode"

	"polling_bot/internal/service"
)

type CommandHandler interface {
    ParseCommand(input string) (command string, args []string, isValid bool)
    HandleCommand(ctx context.Context, command string, args []string, userID string) (string, error)
    GetHelpText() string
}

type PollCommandHandler struct {
	service service.PollService
}

func NewPollCommandHandler(service service.PollService) *PollCommandHandler {
	return &PollCommandHandler{
		service: service,
	}
}

func (h *PollCommandHandler) ParseCommand(input string) (command string, args []string, isValid bool) {
	parts := h.parseCommandArgs(input)
	if len(parts) < 1 || parts[0] != "!poll" {
		return "", nil, false
	}

	if len(parts) < 2 {
		return "help", nil, true
	}

	return strings.ToLower(parts[1]), parts[2:], true
}

func (h *PollCommandHandler) HandleCommand(ctx context.Context, command string, args []string, userID string) (string, error) {
	switch command {
	case "help":
		return h.GetHelpText(), nil

	case "create":
		if len(args) < 2 {
			return "Недостаточно аргументов. Нужен вопрос и хотя бы одна опция", nil
		}
		return h.service.CreatePoll(ctx, userID, args[0], args[1:])

	case "vote":
		if len(args) != 2 {
			return "Формат: !poll vote \"ID опроса\" \"Ваш выбор\"", nil
		}
		return h.service.AddVote(ctx, userID, args[0], args[1])

	case "results":
		if len(args) != 1 {
			return "Формат: !poll results \"ID опроса\"", nil
		}
		return h.service.GetResults(ctx, userID, args[0])

	case "end":
		if len(args) != 1 {
			return "Формат: !poll end \"ID опроса\"", nil
		}
		return h.service.EndPoll(ctx, userID, args[0])

	case "delete":
		if len(args) != 1 {
			return "Формат: !poll delete \"ID опроса\"", nil
		}
		return h.service.DeletePoll(ctx, userID, args[0])

	default:
		return "Неизвестная команда. Введите !poll help для справки", nil
	}
}

func (h *PollCommandHandler) GetHelpText() string {
	return `**Команды опросов:**
    !poll create "Вопрос" "Опция 1" "Опция 2"... - Создать опрос
    !poll vote "ID опроса" "Выбор" - Проголосовать
    !poll results "ID опроса" - Показать результаты
    !poll end "ID опроса" - Завершить опрос
    !poll delete "ID опроса" - Удалить опрос
    !poll help - Показать эту справку`
}

func (h *PollCommandHandler) parseCommandArgs(input string) []string {
	var args []string
	var buf strings.Builder
	inQuotes := false
	var quoteChar rune
	escape := false

	for i, r := range input {
		if escape {
			buf.WriteRune(r)
			escape = false
			continue
		}

		switch {
		case r == '\\':
			if inQuotes {
				escape = true
			} else {
				buf.WriteRune(r)
			}

		case r == '"' || r == '\'':
			if inQuotes {
				if r == quoteChar {
					inQuotes = false
					args = append(args, buf.String())
					buf.Reset()
				} else {
					buf.WriteRune(r)
				}
			} else {
				inQuotes = true
				quoteChar = r
			}

		case unicode.IsSpace(r):
			if inQuotes {
				buf.WriteRune(r)
			} else {
				if buf.Len() > 0 {
					args = append(args, buf.String())
					buf.Reset()
				}
			}

		default:
			buf.WriteRune(r)
		}

		if i == len(input)-1 && buf.Len() > 0 {
			args = append(args, buf.String())
		}
	}

	return args
}