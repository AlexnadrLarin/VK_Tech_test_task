package bot

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"polling_bot/internal/config"
	"polling_bot/internal/handler"

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
	commandHandler handler.CommandHandler
}

func NewBot(cfg config.Config, logger zerolog.Logger, handler handler.CommandHandler) (*Bot, error){
	if cfg.MattermostURL == "" || cfg.BotToken == "" {
        return nil, fmt.Errorf("mattermost URL и токен обязательны для настройки")
    }
    
    normalizedURL := cfg.MattermostURL
    if !strings.HasPrefix(normalizedURL, "http://") && !strings.HasPrefix(normalizedURL, "https://") {
        normalizedURL = "http://" + normalizedURL
    }
    cfg.MattermostURL = normalizedURL

    return &Bot{
        cfg:            cfg,
        logger:         logger,
        commandHandler: handler,
    }, nil
}

func (b *Bot) Start(ctx context.Context) error {
	if err := b.initialize(); err != nil {
		return err
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
		case event := <-b.wsClient.EventChannel():
			wg.Add(1)
			go func(e *model.WebSocketEvent) {
				defer wg.Done()
				b.handleWebSocketEvent(ctx, e)
			}(event)
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
    if b.wsClient != nil {
        return nil
    }

    wsURL := strings.Replace(b.cfg.MattermostURL, "http", "ws", 1)
    wsClient, err := model.NewWebSocketClient4(wsURL, b.cfg.BotToken)
    if err != nil {
        return fmt.Errorf("ошибка создания WebSocket клиента: %w", err)
    }
    
    b.wsClient = NewWSClientAdapter(wsClient)
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

	command, args, isValid := b.commandHandler.ParseCommand(post.Message)
	if !isValid {
		return
	}

	responseMessage, err := b.commandHandler.HandleCommand(ctx, command, args, post.UserId)

	if err != nil {
		b.logger.Error().Err(err).Msg("Ошибка выполнения команды")
		responseMessage = "Ошибка при выполнении команды"
	}

	if responseMessage != "" {
		b.sendResponse(post.ChannelId, responseMessage)
	}
}

func (b *Bot) sendResponse(channelID, message string) {
	response := &model.Post{
		ChannelId: channelID,
		Message:   message,
	}

	if _, resp := b.client.CreatePost(response); resp.Error != nil {
		b.logger.Error().Err(resp.Error).Msg("Ошибка при отправке сообщения")
	}
	b.logger.Info().Msgf("Собщение успешно отправлено по этому ChannelID: %s", channelID)
}
