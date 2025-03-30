package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"polling_bot/internal/config"
	"polling_bot/internal/service"

	"github.com/mattermost/mattermost-server/v5/model"
	"github.com/rs/zerolog"
)

// ########################
// ### Mocks and Fixtures
// ########################

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

// Тест хендлера аргументов
func TestHandleWebSocketEvent(t *testing.T) {
	mockService := service.PollService{}

	var lastPost *model.Post
	fc := &fakeClient{
		getMeFunc: func(param string) (*model.User, *model.Response) {
			return &model.User{Id: "bot123"}, &model.Response{}
		},
		createPostFunc: func(post *model.Post) (*model.Post, *model.Response) {
			lastPost = post
			return post, &model.Response{}
		},
	}

	bot := &Bot{
		service:  mockService,
		logger:   zerolog.Nop(),
		botUser:  &model.User{Id: "bot123"},
		client:   fc,
		wsClient: &fakeWSClient{},
	}

	tests := []struct {
		name        string
		inputMsg    string
		wantMessage string
		setup       func(*Bot)
		inputUser   string
		eventType   string
	}{
		{
			name:        "help command",
			inputMsg:    "!poll help",
			wantMessage: "**Команды опросов:**",
			eventType:   "post",
		},
		{
			name:        "create poll success",
			inputMsg:    `!poll create "Ваш вопрос?" "Вариант 1" "Вариант 2"`,
			wantMessage: "Голосование создано успешно! ID: mock-poll-123",
			eventType:   "post",
		},
		{
			name:        "create poll insufficient args",
			inputMsg:    "!poll create Вопрос",
			wantMessage: "Недостаточно аргументов. Нужен вопрос и хотя бы одна опция",
			eventType:   "post",
		},
		{
			name:        "vote command",
			inputMsg:    `!poll vote "test-poll" "Вариант 1"`,
			wantMessage: "Ваш голос в голосовании test-poll записан: Вариант 1",
			eventType:   "post",
		},
		{
			name:        "results command",
			inputMsg:    `!poll results "test-poll"`,
			wantMessage: "Результаты Голосования (mock):",
			eventType:   "post",
		},
		{
			name:        "end poll success",
			inputMsg:    `!poll end "test-poll"`,
			wantMessage: "Голосование test-poll окончено",
			eventType:   "post",
		},
		{
			name:        "end poll insufficient args",
			inputMsg:    "!poll end",
			wantMessage: "Формат: !poll end \"ID опроса\"",
			eventType:   "post",
		},
		{
			name:        "delete poll success",
			inputMsg:    `!poll delete "test-poll"`,
			wantMessage: "Голосование test-poll удалено",
			eventType:   "post",
		},
		{
			name:        "delete poll insufficient args",
			inputMsg:    "!poll delete",
			wantMessage: "Формат: !poll delete \"ID опроса\"",
			eventType:   "post",
		},
		{
			name:        "unknown command",
			inputMsg:    "!poll invalid",
			wantMessage: "Неизвестная команда. Введите !poll help для справки",
			eventType:   "post",
		},
		{
			name:     "insufficient number of arguments",
			inputMsg: "!poll",
			wantMessage: `**Команды опросов:**
    !poll create "Вопрос" "Опция 1" "Опция 2"... - Создать опрос
    !poll vote "ID опроса" "Выбор" - Проголосовать
    !poll results "ID опроса" - Показать результаты
    !poll end "ID опроса" - Завершить опрос
    !poll delete "ID опроса" - Удалить опрос
    !poll help - Показать эту справку`,
			eventType: "post",
		},
		{
			name:        "empty message",
			inputMsg:    "",
			wantMessage: "",
			eventType:   "post",
		},
		{
			name:        "service error handling",
			inputMsg:    `!poll create "Question"`,
			wantMessage: "Недостаточно аргументов. Нужен вопрос и хотя бы одна опция",
			eventType:   "post",
		},
		{
			name:        "complex arguments parsing",
			inputMsg:    `!poll create 'Вопрос с "разными" кавычками' "Вариант с пробелом"`,
			wantMessage: "Голосование создано успешно! ID: mock-poll-123",
			eventType:   "post",
		},
		{
			name:        "mixed quotes arguments",
			inputMsg:    `!poll vote "test'poll" "Вариант'1"`,
			wantMessage: "Ваш голос в голосовании test'poll записан: Вариант'1",
			eventType:   "post",
		},
		{
			name:        "message from bot itself",
			inputMsg:    "!poll help",
			wantMessage: "",
			setup: func(b *Bot) {
				b.botUser.Id = "current_bot_user"
			},
			inputUser: "current_bot_user",
		},
		{
			name:        "invalid websocket event type",
			inputMsg:    "!poll help",
			wantMessage: "",
			eventType:   "other_event",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lastPost = nil

			post := model.Post{
				ChannelId: "test-channel",
				UserId:    "user123",
				Message:   tt.inputMsg,
			}
			postBytes, _ := json.Marshal(post)
			event := &model.WebSocketEvent{
				Event: model.WEBSOCKET_EVENT_POSTED,
				Data: map[string]interface{}{
					tt.eventType: string(postBytes),
				},
			}

			bot.handleWebSocketEvent(context.Background(), event)

			if tt.wantMessage != "" {
				if lastPost == nil {
					t.Fatal("Сообщение не было отправлено")
				}

				if !strings.Contains(lastPost.Message, tt.wantMessage) {
					t.Errorf("Ожидалось: %q\nПолучено: %q",
						tt.wantMessage, lastPost.Message)
				}
			}
		})
	}
}

// Тест парсера аргументов
func TestParseCommandArgs(t *testing.T) {
	bot := &Bot{logger: zerolog.Nop()}

	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "basic quoted arguments",
			input:    `!poll create "Вопрос с пробелами" "Вариант 1"`,
			expected: []string{"!poll", "create", "Вопрос с пробелами", "Вариант 1"},
		},
		{
			name:     "single quoted arguments",
			input:    `!poll vote 'test-id' 'Мой выбор'`,
			expected: []string{"!poll", "vote", "test-id", "Мой выбор"},
		},
		{
			name:     "unquoted arguments",
			input:    "!poll results simpleID",
			expected: []string{"!poll", "results", "simpleID"},
		},
		{
			name:     "mixed quotes inside arguments",
			input:    `!poll create "Вопрос 'с' кавычками" 'И "другой" вариант'`,
			expected: []string{"!poll", "create", "Вопрос 'с' кавычками", "И \"другой\" вариант"},
		},
		{
			name:     "empty argument",
			input:    `!poll create "" "Вариант"`,
			expected: []string{"!poll", "create", "", "Вариант"},
		},
		{
			name:     "unicode and emoji",
			input:    `!poll create "Тест 𝌆йцукен" "☀️🌙"`,
			expected: []string{"!poll", "create", "Тест 𝌆йцукен", "☀️🌙"},
		},
		{
			name:     "multiple spaces",
			input:    "!poll   create   Вопрос    'Вариант A'",
			expected: []string{"!poll", "create", "Вопрос", "Вариант A"},
		},
		{
			name:     "nested quotes",
			input:    `!poll create "'Смешанные' кавычки"`,
			expected: []string{"!poll", "create", "'Смешанные' кавычки"},
		},
		{
			name:     "no arguments",
			input:    "!poll",
			expected: []string{"!poll"},
		},
		{
			name:     "escape characters (if supported)",
			input:    `!poll create "Экранированные \"кавычки\""`,
			expected: []string{"!poll", "create", `Экранированные "кавычки"`},
		},
		{
			name:     "special characters",
			input:    `!poll create "!@#$%^&*()_+" "{}[];:,.<>/?~"`,
			expected: []string{"!poll", "create", "!@#$%^&*()_+", "{}[];:,.<>/?~"},
		},
		{
			name:     "asian characters",
			input:    `!poll create "日本語のテスト" "한글 테스트"`,
			expected: []string{"!poll", "create", "日本語のテスト", "한글 테스트"},
		},
		{
			name:     "arabic text",
			input:    `!poll create "اختبار العربية"`,
			expected: []string{"!poll", "create", "اختبار العربية"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := bot.parseCommandArgs(tt.input)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("Ожидалось %v, получено %v", tt.expected, got)
			}
		})
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
