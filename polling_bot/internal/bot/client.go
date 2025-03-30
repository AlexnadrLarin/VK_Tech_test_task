package bot

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"polling_bot/internal/config"

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

	post := model.PostFromJson(strings.NewReader(event.GetData()["post"].(string)))
	if post == nil || post.UserId == b.botUser.Id {
		return
	}

	response := &model.Post{
		ChannelId: post.ChannelId,
		Message:   post.Message,
	}

	if _, resp := b.client.CreatePost(response); resp.Error != nil {
		b.logger.Err(resp.Error).Msg("Не удалось отправить сообщение")
	}

	b.logger.Info().Msgf("Cooбщение отправлено: %s", response.Message)
}
