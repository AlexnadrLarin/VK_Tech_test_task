package service_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	
	"github.com/google/uuid"
	
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	
	"polling_bot/internal/models"
	"polling_bot/internal/service"
)

type MockPollRepository struct {
	mock.Mock
}

func (m *MockPollRepository) SavePoll(ctx context.Context, poll models.Poll) error {
	args := m.Called(ctx, poll)
	return args.Error(0)
}

func (m *MockPollRepository) GetPoll(ctx context.Context, pollID string) (models.Poll, error) {
	args := m.Called(ctx, pollID)
	return args.Get(0).(models.Poll), args.Error(1)
}

func (m *MockPollRepository) AddVoteAtomic(ctx context.Context, poll models.Poll) error {
	args := m.Called(ctx, poll)
	return args.Error(0)
}

func (m *MockPollRepository) ClosePoll(ctx context.Context, pollID string) error {
	args := m.Called(ctx, pollID)
	return args.Error(0)
}

func (m *MockPollRepository) DeletePoll(ctx context.Context, pollID string) error {
	args := m.Called(ctx, pollID)
	return args.Error(0)
}

func TestCreatePoll(t *testing.T) {
	tests := []struct {
		name        string
		userID      string
		question    string
		options     []string
		mockSetup   func(*MockPollRepository)
		expected    string
		expectedErr string
	}{
		{
			name:     "successful creation",
			userID:   "user1",
			question: "Test question?",
			options:  []string{"Option1", "Option2"},
			mockSetup: func(m *MockPollRepository) {
				m.On("SavePoll", mock.Anything, mock.Anything).
					Return(nil).
					Run(func(args mock.Arguments) {
						poll := args.Get(1).(models.Poll)
						assert.NotEmpty(t, poll.ID)
						assert.Equal(t, "user1", poll.Creator)
						assert.Equal(t, "Test question?", poll.Question)
						assert.Len(t, poll.Options, 2)
						assert.Equal(t, 0, poll.Options["Option1"])
						assert.Equal(t, 0, poll.Options["Option2"])
					})
			},
			expected: "Голосование создано успешно! ID: `",
		},
		{
			name:        "no options",
			userID:      "user1",
			question:    "Test question?",
			options:     []string{},
			mockSetup:   func(m *MockPollRepository) {},
			expectedErr: "должна быть хотя бы одна опция",
		},
		{
			name:        "question too long",
			userID:      "user1",
			question:    strings.Repeat("a", 255+1),
			options:     []string{"Option1"},
			mockSetup:   func(m *MockPollRepository) {},
			expectedErr: "вопрос слишком длинный",
		},
		{
			name:        "option too long",
			userID:      "user1",
			question:    "Test question?",
			options:     []string{strings.Repeat("a", 100+1)},
			mockSetup:   func(m *MockPollRepository) {},
			expectedErr: "вариант ответа слишком длинный",
		},
		{
			name:        "duplicate options",
			userID:      "user1",
			question:    "Test question?",
			options:     []string{"Option1", "Option1"},
			mockSetup:   func(m *MockPollRepository) {},
			expectedErr: "все опции в голосовании должны быть уникальными",
		},
		{
			name:     "save poll error",
			userID:   "user1",
			question: "Test question?",
			options:  []string{"Option1"},
			mockSetup: func(m *MockPollRepository) {
				m.On("SavePoll", mock.Anything, mock.Anything).
					Return(errors.New("db error"))
			},
			expectedErr: "ошибка сохранения опроса: db error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := new(MockPollRepository)
			tt.mockSetup(mockRepo)

			service := service.NewPollService(mockRepo)
			result, err := service.CreatePoll(context.Background(), tt.userID, tt.question, tt.options)

			if tt.expectedErr != "" {
				assert.ErrorContains(t, err, tt.expectedErr)
				assert.Empty(t, result)
			} else {
				assert.NoError(t, err)
				assert.Contains(t, result, tt.expected)
				assert.Contains(t, result, tt.question)
				for _, opt := range tt.options {
					assert.Contains(t, result, opt)
				}
			}

			mockRepo.AssertExpectations(t)
		})
	}
}

func TestAddVote(t *testing.T) {
	validPollID := uuid.New().String()
	userID := "user1"
	question := "Test question?"

	tests := []struct {
		name        string
		userID      string
		pollID      string
		choice      string
		mockSetup   func(*MockPollRepository)
		expected    string
		expectedErr string
	}{
		{
			name:   "successful vote",
			userID: userID,
			pollID: validPollID,
			choice: "Option1",
			mockSetup: func(m *MockPollRepository) {
				poll := models.Poll{
					ID:       validPollID,
					Creator:  "creator",
					Question: question,
					Options:  map[string]int{"Option1": 0, "Option2": 0},
					Voters:   make(map[string]bool),
					Closed:   false,
				}
				m.On("GetPoll", mock.Anything, validPollID).Return(poll, nil)
				m.On("AddVoteAtomic", mock.Anything, mock.Anything).
					Return(nil).
					Run(func(args mock.Arguments) {
						updatedPoll := args.Get(1).(models.Poll)
						assert.True(t, updatedPoll.Voters[userID])
						assert.Equal(t, 1, updatedPoll.Options["Option1"])
					})
			},
			expected: fmt.Sprintf("Ваш голос в голосовании %s записан: Option1", validPollID),
		},
		{
			name:        "invalid poll ID format",
			userID:      userID,
			pollID:      "invalid-id",
			choice:      "Option1",
			mockSetup:   func(m *MockPollRepository) {},
			expectedErr: "неверный формат ID опроса",
		},
		{
			name:   "poll not found",
			userID: userID,
			pollID: validPollID,
			choice: "Option1",
			mockSetup: func(m *MockPollRepository) {
				m.On("GetPoll", mock.Anything, validPollID).
					Return(models.Poll{}, errors.New("not found"))
			},
			expectedErr: "опрос не найден",
		},
		{
			name:   "poll closed",
			userID: userID,
			pollID: validPollID,
			choice: "Option1",
			mockSetup: func(m *MockPollRepository) {
				poll := models.Poll{
					ID:       validPollID,
					Creator:  "creator",
					Question: question,
					Options:  map[string]int{"Option1": 0, "Option2": 0},
					Voters:   make(map[string]bool),
					Closed:   true,
				}
				m.On("GetPoll", mock.Anything, validPollID).Return(poll, nil)
			},
			expectedErr: "опрос завершен",
		},
		{
			name:   "already voted",
			userID: userID,
			pollID: validPollID,
			choice: "Option1",
			mockSetup: func(m *MockPollRepository) {
				poll := models.Poll{
					ID:       validPollID,
					Creator:  "creator",
					Question: question,
					Options:  map[string]int{"Option1": 0, "Option2": 0},
					Voters:   map[string]bool{userID: true},
					Closed:   false,
				}
				m.On("GetPoll", mock.Anything, validPollID).Return(poll, nil)
			},
			expectedErr: "вы уже голосовали в этом опросе",
		},
		{
			name:   "invalid choice",
			userID: userID,
			pollID: validPollID,
			choice: "InvalidOption",
			mockSetup: func(m *MockPollRepository) {
				poll := models.Poll{
					ID:       validPollID,
					Creator:  "creator",
					Question: question,
					Options:  map[string]int{"Option1": 0, "Option2": 0},
					Voters:   make(map[string]bool),
					Closed:   false,
				}
				m.On("GetPoll", mock.Anything, validPollID).Return(poll, nil)
			},
			expectedErr: "вариант 'InvalidOption' не существует",
		},
		{
			name:   "save vote error",
			userID: userID,
			pollID: validPollID,
			choice: "Option1",
			mockSetup: func(m *MockPollRepository) {
				poll := models.Poll{
					ID:       validPollID,
					Creator:  "creator",
					Question: question,
					Options:  map[string]int{"Option1": 0, "Option2": 0},
					Voters:   make(map[string]bool),
					Closed:   false,
				}
				m.On("GetPoll", mock.Anything, validPollID).Return(poll, nil)
				m.On("AddVoteAtomic", mock.Anything, mock.Anything).
					Return(errors.New("db error"))
			},
			expectedErr: "ошибка сохранения голоса: db error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := new(MockPollRepository)
			tt.mockSetup(mockRepo)

			service := service.NewPollService(mockRepo)
			result, err := service.AddVote(context.Background(), tt.userID, tt.pollID, tt.choice)

			if tt.expectedErr != "" {
				assert.ErrorContains(t, err, tt.expectedErr)
				assert.Empty(t, result)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}

			mockRepo.AssertExpectations(t)
		})
	}
}

func TestGetResults(t *testing.T) {
	validPollID := uuid.New().String()
	question := "Test question?"

	tests := []struct {
		name        string
		userID      string
		pollID      string
		mockSetup   func(*MockPollRepository)
		expected    string
		expectedErr string
	}{
		{
			name:   "successful results",
			userID: "user1",
			pollID: validPollID,
			mockSetup: func(m *MockPollRepository) {
				poll := models.Poll{
					ID:       validPollID,
					Creator:  "creator",
					Question: question,
					Options:  map[string]int{"Option1": 5, "Option2": 3},
					Voters:   make(map[string]bool),
					Closed:   false,
				}
				m.On("GetPoll", mock.Anything, validPollID).Return(poll, nil)
			},
			expected: fmt.Sprintf("**Результаты опроса %s**\n%s\n- Option1: 5 голосов\n- Option2: 3 голосов\n", validPollID, question),
		},
		{
			name:   "poll not found",
			userID: "user1",
			pollID: validPollID,
			mockSetup: func(m *MockPollRepository) {
				m.On("GetPoll", mock.Anything, validPollID).
					Return(models.Poll{}, errors.New("not found"))
			},
			expectedErr: "опрос не найден",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := new(MockPollRepository)
			tt.mockSetup(mockRepo)

			service := service.NewPollService(mockRepo)
			result, err := service.GetResults(context.Background(), tt.userID, tt.pollID)

			if tt.expectedErr != "" {
				assert.ErrorContains(t, err, tt.expectedErr)
				assert.Empty(t, result)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}

			mockRepo.AssertExpectations(t)
		})
	}
}

func TestEndPoll(t *testing.T) {
	validPollID := uuid.New().String()
	creatorID := "creator1"
	otherUserID := "user2"
	question := "Test question?"

	tests := []struct {
		name        string
		userID      string
		pollID      string
		mockSetup   func(*MockPollRepository)
		expected    string
		expectedErr string
	}{
		{
			name:   "successful end by creator",
			userID: creatorID,
			pollID: validPollID,
			mockSetup: func(m *MockPollRepository) {
				poll := models.Poll{
					ID:       validPollID,
					Creator:  creatorID,
					Question: question,
					Options:  map[string]int{"Option1": 5, "Option2": 3},
					Voters:   make(map[string]bool),
					Closed:   false,
				}
				m.On("GetPoll", mock.Anything, validPollID).Return(poll, nil)
				m.On("ClosePoll", mock.Anything, validPollID).Return(nil)
			},
			expected: fmt.Sprintf("Голосование %s окончено", validPollID),
		},
		{
			name:   "poll not found",
			userID: creatorID,
			pollID: validPollID,
			mockSetup: func(m *MockPollRepository) {
				m.On("GetPoll", mock.Anything, validPollID).
					Return(models.Poll{}, errors.New("not found"))
			},
			expectedErr: "опрос не найден",
		},
		{
			name:   "not creator",
			userID: otherUserID,
			pollID: validPollID,
			mockSetup: func(m *MockPollRepository) {
				poll := models.Poll{
					ID:       validPollID,
					Creator:  creatorID,
					Question: question,
					Options:  map[string]int{"Option1": 5, "Option2": 3},
					Voters:   make(map[string]bool),
					Closed:   false,
				}
				m.On("GetPoll", mock.Anything, validPollID).Return(poll, nil)
			},
			expectedErr: "только создатель может завершить опрос",
		},
		{
			name:   "close poll error",
			userID: creatorID,
			pollID: validPollID,
			mockSetup: func(m *MockPollRepository) {
				poll := models.Poll{
					ID:       validPollID,
					Creator:  creatorID,
					Question: question,
					Options:  map[string]int{"Option1": 5, "Option2": 3},
					Voters:   make(map[string]bool),
					Closed:   false,
				}
				m.On("GetPoll", mock.Anything, validPollID).Return(poll, nil)
				m.On("ClosePoll", mock.Anything, validPollID).Return(errors.New("db error"))
			},
			expectedErr: "ошибка завершения опроса: db error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := new(MockPollRepository)
			tt.mockSetup(mockRepo)

			service := service.NewPollService(mockRepo)
			result, err := service.EndPoll(context.Background(), tt.userID, tt.pollID)

			if tt.expectedErr != "" {
				assert.ErrorContains(t, err, tt.expectedErr)
				assert.Empty(t, result)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}

			mockRepo.AssertExpectations(t)
		})
	}
}

func TestDeletePoll(t *testing.T) {
	validPollID := uuid.New().String()
	creatorID := "creator1"
	otherUserID := "user2"
	question := "Test question?"

	tests := []struct {
		name        string
		userID      string
		pollID      string
		mockSetup   func(*MockPollRepository)
		expected    string
		expectedErr string
	}{
		{
			name:   "successful delete by creator",
			userID: creatorID,
			pollID: validPollID,
			mockSetup: func(m *MockPollRepository) {
				poll := models.Poll{
					ID:       validPollID,
					Creator:  creatorID,
					Question: question,
					Options:  map[string]int{"Option1": 5, "Option2": 3},
					Voters:   make(map[string]bool),
					Closed:   false,
				}
				m.On("GetPoll", mock.Anything, validPollID).Return(poll, nil)
				m.On("DeletePoll", mock.Anything, validPollID).Return(nil)
			},
			expected: fmt.Sprintf("Голосование %s удалено", validPollID),
		},
		{
			name:   "poll not found",
			userID: creatorID,
			pollID: validPollID,
			mockSetup: func(m *MockPollRepository) {
				m.On("GetPoll", mock.Anything, validPollID).
					Return(models.Poll{}, errors.New("not found"))
			},
			expectedErr: "опрос не найден",
		},
		{
			name:   "not creator",
			userID: otherUserID,
			pollID: validPollID,
			mockSetup: func(m *MockPollRepository) {
				poll := models.Poll{
					ID:       validPollID,
					Creator:  creatorID,
					Question: question,
					Options:  map[string]int{"Option1": 5, "Option2": 3},
					Voters:   make(map[string]bool),
					Closed:   false,
				}
				m.On("GetPoll", mock.Anything, validPollID).Return(poll, nil)
			},
			expectedErr: "только создатель может удалить опрос",
		},
		{
			name:   "delete poll error",
			userID: creatorID,
			pollID: validPollID,
			mockSetup: func(m *MockPollRepository) {
				poll := models.Poll{
					ID:       validPollID,
					Creator:  creatorID,
					Question: question,
					Options:  map[string]int{"Option1": 5, "Option2": 3},
					Voters:   make(map[string]bool),
					Closed:   false,
				}
				m.On("GetPoll", mock.Anything, validPollID).Return(poll, nil)
				m.On("DeletePoll", mock.Anything, validPollID).Return(errors.New("db error"))
			},
			expectedErr: "ошибка удаления опроса: db error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := new(MockPollRepository)
			tt.mockSetup(mockRepo)

			service := service.NewPollService(mockRepo)
			result, err := service.DeletePoll(context.Background(), tt.userID, tt.pollID)

			if tt.expectedErr != "" {
				assert.ErrorContains(t, err, tt.expectedErr)
				assert.Empty(t, result)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}

			mockRepo.AssertExpectations(t)
		})
	}
}
