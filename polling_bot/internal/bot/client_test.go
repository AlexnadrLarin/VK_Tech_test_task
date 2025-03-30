package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
	"errors"

	"polling_bot/internal/config"

	"github.com/mattermost/mattermost-server/v5/model"
	"github.com/rs/zerolog"
)

// ########################
// ### Mocks and Fixtures
// ########################

type fakeClient struct {
	Transport http.RoundTripper
	getMeFunc      func(string) (*model.User, *model.Response)
	createPostFunc func(*model.Post) (*model.Post, *model.Response)
}

func (f *fakeClient) GetMe(param string) (*model.User, *model.Response) {
	return f.getMeFunc(param)
}

func (f *fakeClient) CreatePost(post *model.Post) (*model.Post, *model.Response) {
	return f.createPostFunc(post)
}

type fakeWSClient struct {
    events    chan *model.WebSocketEvent
	autoClose bool
    sendEvent bool 
}

func (f *fakeWSClient) Listen() {
    go func() {
		if f.autoClose {
			defer close(f.events)
		}
        if f.sendEvent {
            f.events <- &model.WebSocketEvent{Event: "test_event"}
        } 
    }()
}

func (f *fakeWSClient) Close()  {
	close(f.events)
}

func (f *fakeWSClient) EventChannel() <-chan *model.WebSocketEvent {
	return f.events
}

type mockHTTPClient struct{}

func (m *mockHTTPClient) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host == "dummy" && req.URL.Path == "/api/v4/users/me" {
        return &http.Response{
            StatusCode: http.StatusOK,
            Body:       io.NopCloser(bytes.NewReader([]byte(`{"id":"bot123"}`))),
        }, nil
    }
    return nil, fmt.Errorf("mockHTTPClient: unknown request %v", req.URL)
}

// ########################
// ### TestCases
// ########################

// Тесты функций обработки url
// ###########################
func TestNewBot_PrependHTTP(t *testing.T) {
	cfg := config.Config{
		MattermostURL: "mattermost.example.com",
		BotToken:      "dummy",
		HTTPTimeout:   time.Second,
	}
	logger := zerolog.Nop()
	bot := NewBot(cfg, logger)
	if !strings.HasPrefix(bot.cfg.MattermostURL, "http://") {
		t.Errorf("Ожидалось, что URL начнётся с http://, получено: %s", bot.cfg.MattermostURL)
	}
}

func TestNewBot_ValidURL(t *testing.T) {
	cfg := config.Config{
		MattermostURL: "https://mattermost.example.com",
		BotToken:      "dummy",
		HTTPTimeout:   time.Second,
	}
	logger := zerolog.Nop()
	bot := NewBot(cfg, logger)
	if bot.cfg.MattermostURL != "https://mattermost.example.com" {
		t.Errorf("URL не должен изменяться, получено: %s", bot.cfg.MattermostURL)
	}
}

// Тесты функций для иницализации http-клиента
// ###########################################
func TestAuthenticate_Success(t *testing.T) {
	fc := &fakeClient{
		getMeFunc: func(param string) (*model.User, *model.Response) {
			return &model.User{Id: "bot123"}, &model.Response{}
		},
	}
	bot := &Bot{
		client: fc,
		logger: zerolog.Nop(),
	}
	err := bot.authenticate()
	if err != nil {
		t.Errorf("Ожидалась успешная аутентификация, получена ошибка: %v", err)
	}
	if bot.botUser == nil || bot.botUser.Id != "bot123" {
		t.Error("botUser не установлен корректно после аутентификации")
	}
}

func TestAuthenticate_Error(t *testing.T) {
	fc := &fakeClient{
		getMeFunc: func(param string) (*model.User, *model.Response) {
			return nil, &model.Response{Error: &model.AppError{
				Message: "authentication failed",
			}}
		},
	}
	bot := &Bot{
		client: fc,
		logger: zerolog.Nop(),
	}
	err := bot.authenticate()
	if err == nil {
		t.Error("Ожидалась ошибка аутентификации, но получен nil")
	}
}

// Тесты функций для иницализации WebSocket
// ###########################################
func TestInitWebSocket_Success(t *testing.T) {
    cfg := config.Config{
        MattermostURL: "http://example.com",
        BotToken:      "dummy",
        HTTPTimeout:   time.Second,
    }

    bot := NewBot(cfg, zerolog.Nop())
    
    bot.wsClient = &fakeWSClient{
        events: make(chan *model.WebSocketEvent),
    }

    err := bot.initWebSocket()
    if err != nil {
        t.Errorf("Ожидалось, что initWebSocket завершится успешно, получена ошибка: %v", err)
    }
    if bot.wsClient == nil {
        t.Error("Ожидалось, что wsClient будет инициализирован")
    }
}

func TestInitWebSocket_Error(t *testing.T) {
	cfg := config.Config{
		MattermostURL: "", 
		BotToken:      "dummy",
		HTTPTimeout:   time.Second,
	}
	bot := NewBot(cfg, zerolog.Nop())
	err := bot.initWebSocket()
	if err == nil {
		t.Error("Ожидалась ошибка инициализации WebSocket при некорректном URL, но ошибка не получена")
	}
}

// Тесты функций для общей инициализации
// ###########################################
func TestInitialize_Success(t *testing.T) {
	fc := &fakeClient{
		getMeFunc: func(param string) (*model.User, *model.Response) {
			return &model.User{Id: "bot123"}, &model.Response{}
		},
		createPostFunc: func(post *model.Post) (*model.Post, *model.Response) {
			return post, &model.Response{}
		},
		Transport: &mockHTTPClient{}, 
	}

	ws := &fakeWSClient{
		events: make(chan *model.WebSocketEvent),
	}

	cfg := config.Config{
		MattermostURL: "http://dummy",
		BotToken:      "dummy",
		HTTPTimeout:   time.Second,
	}

	bot := NewBot(cfg, zerolog.Nop())
	bot.client = fc 
	bot.botUser = &model.User{Id: "bot123"}
	bot.wsClient = ws

	if apiClient, ok := bot.client.(*fakeClient); ok {
		apiClient.Transport = &mockHTTPClient{} 
	} else {
		t.Fatalf("Expected bot.client to be of type *fakeClient, but got %T", bot.client)
	}

	err := bot.initialize()
	if err != nil {
		t.Errorf("Ожидалось успешное выполнение initialize, получена ошибка: %v", err)
	}
}

// Тесты основной логики
// #####################
func TestStart_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ws := &fakeWSClient{
		events: make(chan *model.WebSocketEvent),
	}

	fc := &fakeClient{
		getMeFunc: func(param string) (*model.User, *model.Response) {
			return &model.User{Id: "bot123"}, &model.Response{}
		},
		createPostFunc: func(post *model.Post) (*model.Post, *model.Response) {
			return post, &model.Response{}
		},
	}
	cfg := config.Config{
		MattermostURL: "http://dummy",
		BotToken:      "dummy",
		HTTPTimeout:   time.Second,
	}

	bot := NewBot(cfg, zerolog.Nop())
	bot.client = fc
	bot.botUser = &model.User{Id: "bot123"}
	bot.wsClient = ws

	errCh := make(chan error, 1)
	go func() {
		errCh <- bot.Start(ctx)
	}()
	time.Sleep(50 * time.Millisecond) 

    cancel()

    select {
    case err := <-errCh:
        if !errors.Is(err, context.Canceled) {
            t.Errorf("Ожидался context.Canceled, получено: %v", err)
        }
    case <-time.After(100 * time.Millisecond):
        t.Error("Тест не дождался завершения")
    }
}

func TestHandleWebSocketEvent(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)
	var postCreated *model.Post

	fc := &fakeClient{
		getMeFunc: func(param string) (*model.User, *model.Response) {
			return &model.User{Id: "bot123"}, &model.Response{}
		},
		createPostFunc: func(post *model.Post) (*model.Post, *model.Response) {
			postCreated = post
			wg.Done()
			return post, &model.Response{}
		},
	}

	logger := zerolog.Nop()
	botInstance := &Bot{
		logger:  logger,
		client:  fc,
		botUser: &model.User{Id: "bot123"},
		wsClient: &fakeWSClient{
			events: make(chan *model.WebSocketEvent, 1),
		},
	}

	originalPost := model.Post{
		ChannelId: "channel456",
		UserId:    "user789",
		Message:   "Привет, бот!",
	}
	postBytes, err := json.Marshal(originalPost)
	if err != nil {
		t.Fatal("Не удалось сериализовать пост:", err)
	}
	event := &model.WebSocketEvent{
		Event: model.WEBSOCKET_EVENT_POSTED,
		Data: map[string]interface{}{
			"post": string(postBytes),
		},
	}

	ctx := context.Background()
	botInstance.handleWebSocketEvent(ctx, event)
	wg.Wait() 

	if postCreated == nil {
		t.Fatal("Ожидался вызов CreatePost, но его не произошло")
	}
	if postCreated.ChannelId != originalPost.ChannelId {
		t.Errorf("Ожидался ChannelId %s, получен %s", originalPost.ChannelId, postCreated.ChannelId)
	}
	if postCreated.Message != originalPost.Message {
		t.Errorf("Ожидалось сообщение %s, получено %s", originalPost.Message, postCreated.Message)
	}
}

func TestHandleWebSocketEventNonPosted(t *testing.T) {
	called := false
	fc := &fakeClient{
		getMeFunc: func(param string) (*model.User, *model.Response) {
			return &model.User{Id: "bot123"}, &model.Response{}
		},
		createPostFunc: func(post *model.Post) (*model.Post, *model.Response) {
			called = true
			return post, &model.Response{}
		},
	}

	botInstance := &Bot{
		logger:  zerolog.Nop(),
		client:  fc,
		botUser: &model.User{Id: "bot123"},
		wsClient: &fakeWSClient{
			events: make(chan *model.WebSocketEvent, 1),
		},
	}

	event := &model.WebSocketEvent{
		Event: "NON_POSTED_EVENT",
		Data:  map[string]interface{}{"post": "{}"},
	}
	ctx := context.Background()
	botInstance.handleWebSocketEvent(ctx, event)
	if called {
		t.Error("CreatePost не должен вызываться для событий, отличных от WEBSOCKET_EVENT_POSTED")
	}
}

func TestHandleWebSocketEventOwnMessage(t *testing.T) {
	called := false
	fc := &fakeClient{
		getMeFunc: func(param string) (*model.User, *model.Response) {
			return &model.User{Id: "bot123"}, &model.Response{}
		},
		createPostFunc: func(post *model.Post) (*model.Post, *model.Response) {
			called = true
			return post, &model.Response{}
		},
	}

	originalPost := model.Post{
		ChannelId: "channel456",
		UserId:    "bot123", 
		Message:   "Привет, бот!",
	}
	postBytes, err := json.Marshal(originalPost)
	if err != nil {
		t.Fatal("Не удалось сериализовать пост:", err)
	}
	event := &model.WebSocketEvent{
		Event: model.WEBSOCKET_EVENT_POSTED,
		Data:  map[string]interface{}{"post": string(postBytes)},
	}

	botInstance := &Bot{
		logger:  zerolog.Nop(),
		client:  fc,
		botUser: &model.User{Id: "bot123"},
		wsClient: &fakeWSClient{
			events: make(chan *model.WebSocketEvent, 1),
		},
	}

	ctx := context.Background()
	botInstance.handleWebSocketEvent(ctx, event)
	if called {
		t.Error("CreatePost не должен вызываться для сообщений, отправленных самим ботом")
	}
}

func TestHandleWebSocketEventCreatePostError(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)
	fc := &fakeClient{
		getMeFunc: func(param string) (*model.User, *model.Response) {
			return &model.User{Id: "bot123"}, &model.Response{}
		},
		createPostFunc: func(post *model.Post) (*model.Post, *model.Response) {
			defer wg.Done()
			return nil, &model.Response{Error: &model.AppError{
				Message: "authentication failed",
			}}
		},
	}

	originalPost := model.Post{
		ChannelId: "channel456",
		UserId:    "user789",
		Message:   "Привет, бот!",
	}
	postBytes, err := json.Marshal(originalPost)
	if err != nil {
		t.Fatal("Не удалось сериализовать пост:", err)
	}
	event := &model.WebSocketEvent{
		Event: model.WEBSOCKET_EVENT_POSTED,
		Data:  map[string]interface{}{"post": string(postBytes)},
	}

	botInstance := &Bot{
		logger:  zerolog.Nop(),
		client:  fc,
		botUser: &model.User{Id: "bot123"},
		wsClient: &fakeWSClient{
			events: make(chan *model.WebSocketEvent, 1),
		},
	}

	ctx := context.Background()
	botInstance.handleWebSocketEvent(ctx, event)
	wg.Wait()
}

// Тест закрытия соединений
// ##################
func TestCloseConnections(t *testing.T) {
	ws := &fakeWSClient{
		events:    make(chan *model.WebSocketEvent),
		autoClose: false,
	}

	bot := &Bot{
		wsClient: ws,
	}

	bot.wsClient.Close()

	select {
	case _, ok := <-ws.events:
		if ok {
			t.Error("EventChannel должен быть закрыт")
		}
	default:
		t.Error("EventChannel не закрыт")
	}
}
