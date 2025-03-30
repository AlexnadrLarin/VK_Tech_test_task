package handler

import (
	"context"
	"testing"
	"errors"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type MockPollService struct {
	mock.Mock
}

func (m *MockPollService) CreatePoll(ctx context.Context, userID, question string, options []string) (string, error) {
	args := m.Called(ctx, userID, question, options)
	return args.String(0), args.Error(1)
}

func (m *MockPollService) AddVote(ctx context.Context, userID, pollID, option string) (string, error) {
	args := m.Called(ctx, userID, pollID, option)
	return args.String(0), args.Error(1)
}

func (m *MockPollService) GetResults(ctx context.Context, userID, pollID string) (string, error) {
	args := m.Called(ctx, userID, pollID)
	return args.String(0), args.Error(1)
}

func (m *MockPollService) EndPoll(ctx context.Context, userID, pollID string) (string, error) {
	args := m.Called(ctx, userID, pollID)
	return args.String(0), args.Error(1)
}

func (m *MockPollService) DeletePoll(ctx context.Context, userID, pollID string) (string, error) {
	args := m.Called(ctx, userID, pollID)
	return args.String(0), args.Error(1)
}

// Тесты для функции ParseCommand
func TestPollCommandHandler_ParseCommand(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantCmd  string
		wantArgs []string
		wantValid bool
	}{
		{
			name:     "Valid create command",
			input:    `!poll create "Favorite Color?" "Red" "Blue"`,
			wantCmd:  "create",
			wantArgs: []string{"Favorite Color?", "Red", "Blue"},
			wantValid: true,
		},
		{
			name:     "Invalid command prefix",
			input:    "!vote create test",
			wantCmd:  "",
			wantArgs: nil,
			wantValid: false,
		},
		{
			name:     "Help command",
			input:    "!poll",
			wantCmd:  "help",
			wantArgs: nil,
			wantValid: true,
		},
		{
			name:     "Mixed quotes and spaces",
			input:    `!poll vote 'Poll 1' "Option A"`,
			wantCmd:  "vote",
			wantArgs: []string{"Poll 1", "Option A"},
			wantValid: true,
		},
	}

	h := NewPollCommandHandler(nil)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, args, valid := h.ParseCommand(tt.input)
			assert.Equal(t, tt.wantCmd, cmd)
			assert.Equal(t, tt.wantArgs, args)
			assert.Equal(t, tt.wantValid, valid)
		})
	}
}

// Тесты для функции HandleCommand
func TestPollCommandHandler_HandleCommand(t *testing.T) {
	ctx := context.Background()
	mockService := new(MockPollService)
	h := NewPollCommandHandler(mockService)

	tests := []struct {
		name        string
		command     string
		args        []string
		mockSetup   func()
		wantMessage string
		wantError   bool
	}{
		{
			name:    "Create poll success",
			command: "create",
			args:    []string{"Question?", "Option1", "Option2"},
			mockSetup: func() {
				mockService.On("CreatePoll", ctx, "user1", "Question?", []string{"Option1", "Option2"}).
					Return("poll123", nil)
			},
			wantMessage: "poll123",
		},
		{
			name:        "Create poll insufficient args",
			command:     "create",
			args:        []string{"Question?"},
			mockSetup:   func() {},
			wantMessage: "Недостаточно аргументов. Нужен вопрос и хотя бы одна опция",
		},
		{
			name:    "Vote success",
			command: "vote",
			args:    []string{"poll123", "Option1"},
			mockSetup: func() {
				mockService.On("AddVote", ctx, "user1", "poll123", "Option1").
					Return("Vote added", nil)
			},
			wantMessage: "Vote added",
		},
		{
			name:        "Vote invalid args",
			command:     "vote",
			args:        []string{"poll123"},
			mockSetup:   func() {},
			wantMessage: "Формат: !poll vote \"ID опроса\" \"Ваш выбор\"",
		},
		{
			name:        "Unknown command",
			command:     "unknown",
			args:        []string{},
			mockSetup:   func() {},
			wantMessage: "Неизвестная команда. Введите !poll help для справки",
		},

		{
			name:        "Help command",
			command:     "help",
			args:        []string{},
			mockSetup:   func() {},
			wantMessage: h.GetHelpText(),
		},
		{
			name:    "Create poll with one option",
			command: "create",
			args:    []string{"Question?", "Option1"},
			mockSetup: func() {
				mockService.On("CreatePoll", ctx, "user1", "Question?", []string{"Option1"}).
					Return("poll456", nil)
			},
			wantMessage: "poll456",
		},
		{
			name:    "Create poll service error",
			command: "create",
			args:    []string{"Q", "O1"},
			mockSetup: func() {
				mockService.On("CreatePoll", ctx, "user1", "Q", []string{"O1"}).
					Return("", errors.New("service error"))
			},
			wantError: true,
		},
		{
			name:        "Vote too many args",
			command:     "vote",
			args:        []string{"poll123", "Option1", "extra"},
			mockSetup:   func() {},
			wantMessage: "Формат: !poll vote \"ID опроса\" \"Ваш выбор\"",
		},
		{
			name:    "Vote service error",
			command: "vote",
			args:    []string{"poll123", "Option1"},
			mockSetup: func() {
				mockService.On("AddVote", ctx, "user1", "poll123", "Option1").
					Return("", errors.New("invalid vote"))
			},
			wantError: true,
		},
		{
			name:        "Results no args",
			command:     "results",
			args:        []string{},
			mockSetup:   func() {},
			wantMessage: "Формат: !poll results \"ID опроса\"",
		},
		{
			name:        "Results too many args",
			command:     "results",
			args:        []string{"poll123", "extra"},
			mockSetup:   func() {},
			wantMessage: "Формат: !poll results \"ID опроса\"",
		},
		{
			name:    "Results success",
			command: "results",
			args:    []string{"poll123"},
			mockSetup: func() {
				mockService.On("GetResults", ctx, "user1", "poll123").
					Return("Results: OK", nil)
			},
			wantMessage: "Results: OK",
		},
		{
			name:    "Results service error",
			command: "results",
			args:    []string{"poll123"},
			mockSetup: func() {
				mockService.On("GetResults", ctx, "user1", "poll123").
					Return("", errors.New("poll not found"))
			},
			wantError: true,
		},
		{
			name:        "End poll no args",
			command:     "end",
			args:        []string{},
			mockSetup:   func() {},
			wantMessage: "Формат: !poll end \"ID опроса\"",
		},
		{
			name:        "End poll too many args",
			command:     "end",
			args:        []string{"poll123", "extra"},
			mockSetup:   func() {},
			wantMessage: "Формат: !poll end \"ID опроса\"",
		},
		{
			name:    "End poll success",
			command: "end",
			args:    []string{"poll123"},
			mockSetup: func() {
				mockService.On("EndPoll", ctx, "user1", "poll123").
					Return("Poll ended", nil)
			},
			wantMessage: "Poll ended",
		},
		{
			name:    "End poll service error",
			command: "end",
			args:    []string{"poll123"},
			mockSetup: func() {
				mockService.On("EndPoll", ctx, "user1", "poll123").
					Return("", errors.New("unauthorized"))
			},
			wantError: true,
		},
		{
			name:        "Delete poll no args",
			command:     "delete",
			args:        []string{},
			mockSetup:   func() {},
			wantMessage: "Формат: !poll delete \"ID опроса\"",
		},
		{
			name:        "Delete poll too many args",
			command:     "delete",
			args:        []string{"poll123", "extra"},
			mockSetup:   func() {},
			wantMessage: "Формат: !poll delete \"ID опроса\"",
		},
		{
			name:    "Delete poll success",
			command: "delete",
			args:    []string{"poll123"},
			mockSetup: func() {
				mockService.On("DeletePoll", ctx, "user1", "poll123").
					Return("Poll deleted", nil)
			},
			wantMessage: "Poll deleted",
		},
		{
			name:    "Delete poll service error",
			command: "delete",
			args:    []string{"poll123"},
			mockSetup: func() {
				mockService.On("DeletePoll", ctx, "user1", "poll123").
					Return("", errors.New("not found"))
			},
			wantError: true,
		},
		{
			name:        "Uppercase command treated as unknown",
			command:     "CREATE",
			args:        []string{"Q", "O1"},
			mockSetup:   func() {},
			wantMessage: "Неизвестная команда. Введите !poll help для справки",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockService.ExpectedCalls = nil
			tt.mockSetup()

			msg, err := h.HandleCommand(ctx, tt.command, tt.args, "user1")
			
			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			assert.Contains(t, msg, tt.wantMessage)
			mockService.AssertExpectations(t)
		})
	}
}

// Тесты для функции парсинга аргументов, переданных пользователем
func TestParseCommandArgs(t *testing.T) {
    mockService := new(MockPollService)
    h := NewPollCommandHandler(mockService)

    tests := []struct {
        name  string
        input string
        want  []string
    }{
        {
            name:  "Simple args",
            input: `!poll test arg1 arg2 arg3`,
            want:  []string{"arg1", "arg2", "arg3"},
        },
        {
            name:  "Quoted args",
            input: `!poll create "arg 1" 'arg 2' "arg'3"`,
            want:  []string{"arg 1", "arg 2", "arg'3"},
        },
        {
            name:  "Escaped characters",
            input: `!poll vote "arg\"1" "arg\\2"`,
            want:  []string{`arg"1`, `arg\2`},
        },
        {
            name:  "Mixed quotes",
            input: `!poll results 'arg"1' "arg'2"`,
            want:  []string{`arg"1`, `arg'2`},
        },
        {
            name:  "Trailing space",
            input: `!poll end  arg1  arg2  `,
            want:  []string{"arg1", "arg2"},
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            _, args, _ := h.ParseCommand(tt.input)
            assert.Equal(t, tt.want, args)
        })
    }
}

// Тесты для функции, генерирующей сообщения о командах, доступных в боте
func TestGetHelpText(t *testing.T) {
	h := NewPollCommandHandler(nil)
	helpText := h.GetHelpText()

	assert.Contains(t, helpText, "!poll create")
	assert.Contains(t, helpText, "!poll vote")
	assert.Contains(t, helpText, "!poll results")
	assert.Contains(t, helpText, "!poll end")
	assert.Contains(t, helpText, "!poll delete")
	assert.Contains(t, helpText, "!poll help")
}