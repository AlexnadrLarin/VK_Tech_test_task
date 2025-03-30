package bot

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode"

	"polling_bot/internal/config"
	"polling_bot/internal/service"

	"github.com/mattermost/mattermost-server/v5/model"
	"github.com/rs/zerolog"
)

type MattermostClient interface {
	GetMe(string) (*model.User, *model.Response)
	CreatePost(*model.Post) (*model.Post, *model.Response)
}

type APIv4Client struct {
	*model.Client4
}

func NewAPIv4Client(url, token string, timeout time.Duration) *APIv4Client {
	client := model.NewAPIv4Client(url)
	client.SetToken(token)
	client.HttpClient = &http.Client{Timeout: timeout}
	return &APIv4Client{Client4: client}
}

func (c *APIv4Client) GetMe(param string) (*model.User, *model.Response) {
	return c.Client4.GetMe(param)
}

func (c *APIv4Client) CreatePost(post *model.Post) (*model.Post, *model.Response) {
	return c.Client4.CreatePost(post)
}

type WebSocketClient interface {
	Listen()
	Close() 
	EventChannel() <-chan *model.WebSocketEvent
}

type WSClientAdapter struct {
	client *model.WebSocketClient
}

func NewWSClientAdapter(wsClient *model.WebSocketClient) *WSClientAdapter {
	return &WSClientAdapter{client: wsClient}
}

func (w *WSClientAdapter) Listen() {
	w.client.Listen()
}

func (w *WSClientAdapter) Close() {
	w.client.Close()
}

func (w *WSClientAdapter) EventChannel() <-chan *model.WebSocketEvent {
	return w.client.EventChannel
}

type Bot struct {
	cfg      config.Config
	logger   zerolog.Logger
	client   MattermostClient
	wsClient WebSocketClient
	botUser  *model.User
	service service.PollService
}

func NewBot(cfg config.Config, logger zerolog.Logger) *Bot {
	if !strings.HasPrefix(cfg.MattermostURL, "http://") && !strings.HasPrefix(cfg.MattermostURL, "https://") {
		cfg.MattermostURL = "http://" + cfg.MattermostURL
	}

	return &Bot{
		cfg:    cfg,
		logger: logger,
	}
}

func (b *Bot) Start(ctx context.Context) error {
	if err := b.initialize(); err != nil {
		return err
	}

	if b.wsClient == nil {
		return fmt.Errorf("websocket клиент не инициализирован")
	}

	var wg sync.WaitGroup
	defer func() {
		wg.Wait()
		b.wsClient.Close()
	}()

	b.wsClient.Listen()
	b.logger.Info().Msg("Бот запущен")

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event, ok := <-b.wsClient.EventChannel():
			if !ok {
				return fmt.Errorf("EventChannel закрыт")
			}
			wg.Add(1)
			go func(ctx context.Context, e *model.WebSocketEvent, ) {
				defer wg.Done()
					b.handleWebSocketEvent(ctx, e)
			}(ctx, event)
		}
	}
}

func (b *Bot) initialize() error {
	if b.client == nil {
		b.client = NewAPIv4Client(b.cfg.MattermostURL, b.cfg.BotToken, b.cfg.HTTPTimeout)
	}

	if err := b.authenticate(); err != nil {
		return err
	}

	if err := b.initWebSocket(); err != nil {
		return err
	}

	return nil
}

func (b *Bot) authenticate() error {
	user, resp := b.client.GetMe("")
	if resp.Error != nil {
		return resp.Error
	}
	b.botUser = user

	return nil
}

func (b *Bot) initWebSocket() error {
	if b.wsClient == nil {
		wsURL := strings.Replace(b.cfg.MattermostURL, "http", "ws", 1)
		wsClient, err := model.NewWebSocketClient4(wsURL, b.cfg.BotToken)
		if err != nil {
			return err
		}
		b.wsClient = NewWSClientAdapter(wsClient)
	}

	return nil
}


func (b *Bot) handleWebSocketEvent(ctx context.Context, event *model.WebSocketEvent) {
    if event.EventType() != model.WEBSOCKET_EVENT_POSTED {
        return
    }

    data := event.GetData()
    rawPost, ok := data["post"].(string)
    if !ok {
        return
    }

    post := model.PostFromJson(strings.NewReader(rawPost))
    if post == nil || post.UserId == b.botUser.Id {
        return
    }

    args := b.parseCommandArgs(post.Message)
    if len(args) < 1 || args[0] != "!poll" {
        return
    }

	response := &model.Post{
        ChannelId: post.ChannelId,
    }

	if len(args) < 2 {
        b.sendHelpResponse(response)
        return
    }

    command := strings.ToLower(args[1])
    cmdArgs := args[2:]

    var err error
    switch command {
    case "help":
        b.sendHelpResponse(response)
        return
    case "create":
        if len(cmdArgs) < 2 {
            response.Message = "Недостаточно аргументов. Нужен вопрос и хотя бы одна опция"
            break
        }
        response.Message, err = b.service.CreatePoll(ctx, post.UserId, cmdArgs[0], cmdArgs[1:])

    case "vote":
        if len(cmdArgs) != 2 {
            response.Message = "Формат: !poll vote \"ID опроса\" \"Ваш выбор\""
            break
        }
        response.Message, err = b.service.AddVote(ctx, post.UserId, cmdArgs[0], cmdArgs[1])

    case "results":
        if len(cmdArgs) != 1 {
            response.Message = "Формат: !poll results \"ID опроса\""
            break
        }
        response.Message, err = b.service.GetResults(ctx, post.UserId, cmdArgs[0])

    case "end":
        if len(cmdArgs) != 1 {
            response.Message = "Формат: !poll end \"ID опроса\""
            break
        }
        response.Message, err = b.service.EndPoll(ctx, post.UserId, cmdArgs[0])

    case "delete":
        if len(cmdArgs) != 1 {
            response.Message = "Формат: !poll delete \"ID опроса\""
            break
        }
        response.Message, err = b.service.DeletePoll(ctx, post.UserId, cmdArgs[0])

    default:
        response.Message = "Неизвестная команда. Введите !poll help для справки"
    }

    if err != nil {
		response.Message = "Ошибка при формировании ответа"
        b.logger.Err(err).Msg(response.Message)
    }

    b.handleResponse(response)
}

func (b *Bot) parseCommandArgs(input string) []string {
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

        if i == len(input)-1 {
            if buf.Len() > 0 {
                args = append(args, buf.String())
            }
        }
    }

    return args
}

func (b *Bot) sendHelpResponse(response *model.Post) {
	helpText := `**Команды опросов:**
    !poll create "Вопрос" "Опция 1" "Опция 2"... - Создать опрос
    !poll vote "ID опроса" "Выбор" - Проголосовать
    !poll results "ID опроса" - Показать результаты
    !poll end "ID опроса" - Завершить опрос
    !poll delete "ID опроса" - Удалить опрос
    !poll help - Показать эту справку`

	response.Message = helpText

	b.handleResponse(response)
}

func (b *Bot) handleResponse(response *model.Post) {
	if response.Message == "" {
		b.logger.Error().Msg("Ошибка при отправлении запроса: пустое сообщение")
		return
	}

	if _, resp := b.client.CreatePost(response); resp.Error != nil {
		b.logger.Err(resp.Error).Msg("Ошибка при отправлении запроса")
		return
	}

	b.logger.Info().
		Str("Сhannel_id", response.ChannelId).
		Msg("Сообщение успешно отправлено")
}
