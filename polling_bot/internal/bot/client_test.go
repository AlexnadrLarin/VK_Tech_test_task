package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"polling_bot/internal/config"

	"github.com/mattermost/mattermost-server/v5/model"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/mock"
)

// Mocks 
type fakeClient struct {
	Transport      http.RoundTripper
	getMeFunc      func(string) (*model.User, *model.Response)
	createPostFunc func(*model.Post) (*model.Post, *model.Response)
}

func (f *fakeClient) GetMe(param string) (*model.User, *model.Response) {
	return f.getMeFunc(param)
}

func (f *fakeClient) CreatePost(post *model.Post) (*model.Post, *model.Response) {
	if f.createPostFunc != nil {
		return f.createPostFunc(post)
	}
	return post, &model.Response{}
}

type fakeWSClient struct {
	events chan *model.WebSocketEvent
}

func (f *fakeWSClient) Listen() {
}

func (f *fakeWSClient) Close() {
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

type MockCommandHandler struct {
	mock.Mock
}

func (m *MockCommandHandler) ParseCommand(input string) (string, []string, bool) {
	args := m.Called(input)
	return args.String(0), args.Get(1).([]string), args.Bool(2)
}

func (m *MockCommandHandler) HandleCommand(ctx context.Context, command string, args []string, userID string) (string, error) {
	arguments := m.Called(ctx, command, args, userID)
	return arguments.String(0), arguments.Error(1)
}

func (m *MockCommandHandler) GetHelpText() string {
	return m.Called().String(0)
}

// TestCases

// TestNewBot_PrependHTTP проверяет, что при отсутствии префикса "http://" URL корректно дополняется.
func TestNewBot_PrependHTTP(t *testing.T) {
	cfg := config.Config{
		MattermostURL: "mattermost.example.com",
		BotToken:      "dummy",
		HTTPTimeout:   time.Second,
	}
	logger := zerolog.Nop()
	mockHandler := new(MockCommandHandler)
	bot, _ := NewBot(cfg, logger, mockHandler)
	if !strings.HasPrefix(bot.cfg.MattermostURL, "http://") {
		t.Errorf("Ожидалось, что URL начнётся с http://, получено: %s", bot.cfg.MattermostURL)
	}
}

// TestNewBot_ValidURL проверяет, что корректный URL не изменяется.
func TestNewBot_ValidURL(t *testing.T) {
	cfg := config.Config{
		MattermostURL: "https://mattermost.example.com",
		BotToken:      "dummy",
		HTTPTimeout:   time.Second,
	}
	logger := zerolog.Nop()
	mockHandler := new(MockCommandHandler)
	bot, _ := NewBot(cfg, logger, mockHandler)
	if bot.cfg.MattermostURL != "https://mattermost.example.com" {
		t.Errorf("URL не должен изменяться, получено: %s", bot.cfg.MattermostURL)
	}
}

// TestAuthenticate_Success проверяет успешную аутентификацию бота.
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

// TestAuthenticate_Error проверяет обработку ошибки аутентификации.
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

// TestInitWebSocket_Success проверяет успешную инициализацию WebSocket клиента.
func TestInitWebSocket_Success(t *testing.T) {
	cfg := config.Config{
		MattermostURL: "http://example.com",
		BotToken:      "dummy",
		HTTPTimeout:   time.Second,
	}

	mockHandler := new(MockCommandHandler)
	bot, _ := NewBot(cfg, zerolog.Nop(), mockHandler)

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

// TestInitWebSocket_InvalidToken проверяет, что при неверном токене инициализация WebSocket завершится ошибкой.
func TestInitWebSocket_InvalidToken(t *testing.T) {
	cfg := config.Config{
		MattermostURL: "ws://valid.url",
		BotToken:      "invalid_token",
		HTTPTimeout:   time.Second,
	}

	bot := &Bot{
		cfg:    cfg,
		logger: zerolog.Nop(),
	}

	err := bot.initWebSocket()
	if err == nil {
		t.Error("Ожидалась ошибка аутентификации WebSocket")
	}
}

// TestInitialize_Success проверяет общую инициализацию бота.
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

	mockHandler := new(MockCommandHandler)
	bot, _ := NewBot(cfg, zerolog.Nop(), mockHandler)
	bot.client = fc
	bot.botUser = &model.User{Id: "bot123"}
	bot.wsClient = ws

	if apiClient, ok := bot.client.(*fakeClient); ok {
		apiClient.Transport = &mockHTTPClient{}
	} else {
		t.Fatalf("Ожидалась ошибка типа *fakeClient, но получено %T", bot.client)
	}

	err := bot.initialize()
	if err != nil {
		t.Errorf("Ожидалось успешное выполнение initialize, получена ошибка: %v", err)
	}
}

// TestHandleWebSocketEvent проверяет корректную обработку входящего события с командой.
func TestHandleWebSocketEvent(t *testing.T) {
	mockHandler := new(MockCommandHandler)
	logger := zerolog.Nop()

	tests := []struct {
		name          string
		inputMsg      string
		mockSetup     func(*MockCommandHandler)
		expectedCalls int
		wantMessage   string
	}{
		{
			name:     "valid create command",
			inputMsg: `!poll create "Question" "Option1"`,
			mockSetup: func(m *MockCommandHandler) {
				m.On("ParseCommand", `!poll create "Question" "Option1"`).
					Return("create", []string{"Question", "Option1"}, true).
					Once()
				m.On("HandleCommand", mock.Anything, "create", []string{"Question", "Option1"}, "user123").
					Return("Poll created", nil).
					Once()
			},
			expectedCalls: 1,
			wantMessage:   "Poll created",
		},
		{
			name:     "invalid command",
			inputMsg: "invalid command",
			mockSetup: func(m *MockCommandHandler) {
				m.On("ParseCommand", "invalid command").
					Return("", []string{}, false).
					Once()
			},
			expectedCalls: 1,
			wantMessage:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockHandler.ExpectedCalls = nil
			if tt.mockSetup != nil {
				tt.mockSetup(mockHandler)
			}

			var lastPost *model.Post
			fc := &fakeClient{
				createPostFunc: func(post *model.Post) (*model.Post, *model.Response) {
					lastPost = post
					return post, &model.Response{}
				},
			}

			bot := &Bot{
				commandHandler: mockHandler,
				logger:         logger,
				botUser:        &model.User{Id: "bot123"},
				client:         fc,
			}

			post := model.Post{
				ChannelId: "test-channel",
				UserId:    "user123",
				Message:   tt.inputMsg,
			}
			postBytes, _ := json.Marshal(&post)

			event := &model.WebSocketEvent{
				Event: model.WEBSOCKET_EVENT_POSTED,
				Data: map[string]interface{}{
					"post": string(postBytes),
				},
			}

			bot.handleWebSocketEvent(context.Background(), event)

			if tt.wantMessage != "" {
				if lastPost == nil {
					t.Fatal("Сообщение не было отправлено")
				}
				if !strings.Contains(lastPost.Message, tt.wantMessage) {
					t.Errorf("Ожидалось: %q\nПолучено: %q", tt.wantMessage, lastPost.Message)
				}
			} else if lastPost != nil {
				t.Error("Сообщение было отправлено, хотя не должно было")
			}

			mockHandler.AssertNumberOfCalls(t, "HandleCommand", tt.expectedCalls)
			mockHandler.AssertExpectations(t)
		})
	}
}

// TestHandleWebSocketEventEdgeCases проверяет обработку некорректных входящих данных.
func TestHandleWebSocketEventEdgeCases(t *testing.T) {
	mockHandler := new(MockCommandHandler)
	logger := zerolog.Nop()

	tests := []struct {
		name        string
		eventSetup  func() *model.WebSocketEvent
		expectError bool
	}{
		{
			name: "invalid post data",
			eventSetup: func() *model.WebSocketEvent {
				return &model.WebSocketEvent{
					Event: model.WEBSOCKET_EVENT_POSTED,
					Data:  map[string]interface{}{"post": 123},
				}
			},
		},
		{
			name: "message from bot itself",
			eventSetup: func() *model.WebSocketEvent {
				post := model.Post{
					UserId: "bot123",
				}
				postBytes, _ := json.Marshal(&post)
				return &model.WebSocketEvent{
					Event: model.WEBSOCKET_EVENT_POSTED,
					Data:  map[string]interface{}{"post": string(postBytes)},
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var called bool
			fc := &fakeClient{
				createPostFunc: func(post *model.Post) (*model.Post, *model.Response) {
					called = true
					return post, &model.Response{}
				},
			}

			bot := &Bot{
				commandHandler: mockHandler,
				logger:         logger,
				botUser:        &model.User{Id: "bot123"},
				client:         fc,
			}

			event := tt.eventSetup()
			bot.handleWebSocketEvent(context.Background(), event)

			if called {
				t.Error("CreatePost был вызван неожиданно")
			}
		})
	}
}

// TestHandleWebSocketEventNonPosted проверяет, что события, отличные от WEBSOCKET_EVENT_POSTED, игнорируются.
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

// TestHandleWebSocketEventOwnMessage проверяет, что сообщения, отправленные самим ботом, игнорируются.
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
	postBytes, err := json.Marshal(&originalPost)
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



// TestStart_CtxCanceled проверяет, что функция Start корректно завершается при отмене контекста.
func TestStart_CtxCanceled(t *testing.T) {
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
		events: make(chan *model.WebSocketEvent, 1),
	}

	cfg := config.Config{
		MattermostURL: "http://dummy",
		BotToken:      "dummy",
		HTTPTimeout:   time.Second,
	}

	mockHandler := new(MockCommandHandler)
	bot, _ := NewBot(cfg, zerolog.Nop(), mockHandler)
	bot.client = fc
	bot.botUser = &model.User{Id: "bot123"}
	bot.wsClient = ws

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := bot.Start(ctx)
	if err == nil {
		t.Errorf("Ожидалась ошибка отмены контекста, но ошибок не возникло")
	} else if err != context.Canceled {
		t.Errorf("Ожидалась ошибка context.Canceled, но получена: %v", err)
	}
}

// TestStart_AuthFail проверяет, что Start завершается с ошибкой при неуспешной аутентификации.
func TestStart_AuthFail(t *testing.T) {
	fc := &fakeClient{
		getMeFunc: func(param string) (*model.User, *model.Response) {
			return nil, &model.Response{Error: &model.AppError{Message: "authentication failed"}}
		},
	}

	cfg := config.Config{
		MattermostURL: "http://dummy",
		BotToken:      "dummy",
		HTTPTimeout:   time.Second,
	}

	mockHandler := new(MockCommandHandler)
	bot, _ := NewBot(cfg, zerolog.Nop(), mockHandler)
	bot.client = fc

	err := bot.Start(context.Background())
	if err == nil {
		t.Error("Ожидалась ошибка при аутентификации, но получен nil")
	}
}

// TestStart_WebSocketFail проверяет, что Start завершается с ошибкой при неуспешной инициализации WebSocket.
func TestStart_WebSocketFail(t *testing.T) {
	fc := &fakeClient{
		getMeFunc: func(param string) (*model.User, *model.Response) {
			return &model.User{Id: "bot123"}, &model.Response{}
		},
	}

	cfg := config.Config{
		MattermostURL: "http://dummy",
		BotToken:      "dummy",
		HTTPTimeout:   time.Second,
	}

	mockHandler := new(MockCommandHandler)
	bot, _ := NewBot(cfg, zerolog.Nop(), mockHandler)
	bot.client = fc
	bot.botUser = &model.User{Id: "bot123"}
	bot.wsClient = nil

	err := bot.Start(context.Background())
	if err == nil {
		t.Error("Ожидалась ошибка при инициализации WebSocket, но получен nil")
	}
}

// TestCloseConnections проверяет, что метод Close корректно закрывает канал событий.
func TestCloseConnections(t *testing.T) {
	ws := &fakeWSClient{
		events: make(chan *model.WebSocketEvent),
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
