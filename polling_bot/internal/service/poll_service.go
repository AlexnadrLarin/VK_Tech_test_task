package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

type Poll struct {
	ID        string
	Question  string
	Options   map[string]int
	Creator   string
	CreatedAt time.Time
	Closed    bool
}

type PollRepository interface {
	SavePoll(ctx context.Context, poll Poll) error
	GetPoll(ctx context.Context, id string) (Poll, error)
	DeletePoll(ctx context.Context, id string) error
}

type InMemoryPollStorage struct {
	mu    sync.RWMutex
	polls map[string]Poll
}

func NewInMemoryPollStorage() *InMemoryPollStorage {
	return &InMemoryPollStorage{
		polls: make(map[string]Poll),
	}
}

func (s *InMemoryPollStorage) SavePoll(ctx context.Context, poll Poll) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.polls[poll.ID] = poll
	return nil
}

func (s *InMemoryPollStorage) GetPoll(ctx context.Context, id string) (Poll, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	poll, exists := s.polls[id]
	if !exists {
		return Poll{}, errors.New("poll not found")
	}
	return poll, nil
}

func (s *InMemoryPollStorage) DeletePoll(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.polls, id)
	return nil
}

type PollService interface {
    CreatePoll(ctx context.Context, userID, question string, options []string) (string, error)
    AddVote(ctx context.Context, userID, pollID, choice string) (string, error)
    GetResults(ctx context.Context, userID, pollID string) (string, error)
    EndPoll(ctx context.Context, userID, pollID string) (string, error)
    DeletePoll(ctx context.Context, userID, pollID string) (string, error)
}

type PollServiceImpl struct {
	repo      PollRepository
	idGenerator func() string
}

func NewPollService(repo PollRepository) *PollServiceImpl {
	return &PollServiceImpl{
		repo: repo,
		idGenerator: func() string {
			return fmt.Sprintf("poll-%d", time.Now().UnixNano())
		},
	}
}

func (s *PollServiceImpl) CreatePoll(ctx context.Context, userID, question string, options []string) (string, error) {
	if len(options) < 1 {
		return "", errors.New("должна быть хотя бы одна опция")
	}

	poll := Poll{
		ID:        s.idGenerator(),
		Question:  question,
		Options:   make(map[string]int),
		Creator:   userID,
		CreatedAt: time.Now(),
		Closed:    false,
	}

	for _, option := range options {
		poll.Options[option] = 0
	}

	if err := s.repo.SavePoll(ctx, poll); err != nil {
		return "", fmt.Errorf("ошибка сохранения опроса: %w", err)
	}

	return fmt.Sprintf("Голосование создано успешно! ID: `%s`\nВопрос: %s\nВарианты: %v", 
		poll.ID, poll.Question, options), nil
}

func (s *PollServiceImpl) AddVote(ctx context.Context, userID string, pollID string, choice string) (string, error) {
	poll, err := s.repo.GetPoll(ctx, pollID)
	if err != nil {
		return "", errors.New("опрос не найден")
	}

	if poll.Closed {
		return "", errors.New("опрос завершен")
	}

	if _, exists := poll.Options[choice]; !exists {
		return "", fmt.Errorf("вариант '%s' не существует", choice)
	}

	poll.Options[choice]++
	if err := s.repo.SavePoll(ctx, poll); err != nil {
		return "", fmt.Errorf("ошибка сохранения голоса: %w", err)
	}

	return fmt.Sprintf("Ваш голос в голосовании %s записан: %s", pollID, choice), nil
}

func (s *PollServiceImpl) GetResults(ctx context.Context, userID string, pollID string) (string, error) {
	poll, err := s.repo.GetPoll(ctx, pollID)
	if err != nil {
		return "", errors.New("опрос не найден")
	}

	result := fmt.Sprintf("**Результаты опроса %s**\n%s\n", pollID, poll.Question)
	for option, count := range poll.Options {
		result += fmt.Sprintf("- %s: %d голосов\n", option, count)
	}
	return result, nil
}

func (s *PollServiceImpl) EndPoll(ctx context.Context, userID string, pollID string) (string, error) {
	poll, err := s.repo.GetPoll(ctx, pollID)
	if err != nil {
		return "", errors.New("опрос не найден")
	}

	if poll.Creator != userID {
		return "", errors.New("только создатель может завершить опрос")
	}

	poll.Closed = true
	if err := s.repo.SavePoll(ctx, poll); err != nil {
		return "", fmt.Errorf("ошибка завершения опроса: %w", err)
	}

	return fmt.Sprintf("Голосование %s окончено", pollID), nil
}

func (s *PollServiceImpl) DeletePoll(ctx context.Context, userID string, pollID string) (string, error) {
	poll, err := s.repo.GetPoll(ctx, pollID)
	if err != nil {
		return "", errors.New("опрос не найден")
	}

	if poll.Creator != userID {
		return "", errors.New("только создатель может удалить опрос")
	}

	if err := s.repo.DeletePoll(ctx, pollID); err != nil {
		return "", fmt.Errorf("ошибка удаления опроса: %w", err)
	}

	return fmt.Sprintf("Голосование %s удалено", pollID), nil
}
