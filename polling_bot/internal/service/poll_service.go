package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"polling_bot/internal/models"
	"polling_bot/internal/repository"

)

var pollIDRegex = regexp.MustCompile(`^[a-f0-9\-]{36}$`)
const (
	maxQuestionLength = 255
	maxOptionLength   = 100
)

type PollService interface {
	CreatePoll(ctx context.Context, userID, question string, options []string) (string, error)
	AddVote(ctx context.Context, userID, pollID, choice string) (string, error)
	GetResults(ctx context.Context, userID, pollID string) (string, error)
	EndPoll(ctx context.Context, userID, pollID string) (string, error)
	DeletePoll(ctx context.Context, userID, pollID string) (string, error)
}

type PollServiceImpl struct {
	repo repository.PollRepository
}

func NewPollService(repo repository.PollRepository) *PollServiceImpl {
	return &PollServiceImpl{repo: repo}
}

func (s *PollServiceImpl) CreatePoll(ctx context.Context, userID, question string, options []string) (string, error) {
	if len(options) < 1 {
		return "", errors.New("должна быть хотя бы одна опция")
	}
	if len(question) > maxQuestionLength {
		return "", errors.New("вопрос слишком длинный")
	}
	for _, option := range options {
		if len(option) > maxOptionLength {
			return "", errors.New("вариант ответа слишком длинный")
		}
	}

	poll := models.Poll{
		ID:       uuid.New().String(),
		Creator:  userID,
		Question: question,
		Options:  make(map[string]int),
		Voters:   make(map[string]bool),
		Closed:   false,
	}

	for _, option := range options {
		if _, exists := poll.Options[option]; exists {
			return "", errors.New("все опции в голосовании должны быть уникальными")
		}
		poll.Options[option] = 0
	}

	if err := s.repo.SavePoll(ctx, poll); err != nil {
		return "", fmt.Errorf("ошибка сохранения опроса: %w", err)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Голосование создано успешно! ID: `%s`\nВопрос: %s\nВарианты:\n", poll.ID, poll.Question))
	for i, option := range options {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, option))
	}

	return sb.String(), nil
}

func (s *PollServiceImpl) AddVote(ctx context.Context, userID, pollID, choice string) (string, error) {
	if !pollIDRegex.MatchString(pollID) {
		return "", errors.New("неверный формат ID опроса")
	}

	poll, err := s.repo.GetPoll(ctx, pollID)
	if err != nil {
		return "", errors.New("опрос не найден")
	}
	if poll.Closed {
		return "", errors.New("опрос завершен")
	}
	if poll.Voters[userID] {
		return "", errors.New("вы уже голосовали в этом опросе")
	}
	if _, exists := poll.Options[choice]; !exists {
		return "", fmt.Errorf("вариант '%s' не существует", choice)
	}

	poll.Voters[userID] = true
	poll.Options[choice]++
	if err := s.repo.AddVoteAtomic(ctx, poll); err != nil {
		return "", fmt.Errorf("ошибка сохранения голоса: %w", err)
	}

	return fmt.Sprintf("Ваш голос в голосовании %s записан: %s", pollID, choice), nil
}

func (s *PollServiceImpl) GetResults(ctx context.Context, userID, pollID string) (string, error) {
	poll, err := s.repo.GetPoll(ctx, pollID)
	if err != nil {
		return "", errors.New("опрос не найден")
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**Результаты опроса %s**\n%s\n", pollID, poll.Question))
	for option, count := range poll.Options {
		sb.WriteString(fmt.Sprintf("- %s: %d голосов\n", option, count))
	}
	return sb.String(), nil
}

func (s *PollServiceImpl) EndPoll(ctx context.Context, userID, pollID string) (string, error) {
	poll, err := s.repo.GetPoll(ctx, pollID)
	if err != nil {
		return "", errors.New("опрос не найден")
	}
	if poll.Creator != userID {
		return "", errors.New("только создатель может завершить опрос")
	}

	if err := s.repo.ClosePoll(ctx, pollID); err != nil {
		return "", fmt.Errorf("ошибка завершения опроса: %w", err)
	}
	return fmt.Sprintf("Голосование %s окончено", pollID), nil
}

func (s *PollServiceImpl) DeletePoll(ctx context.Context, userID, pollID string) (string, error) {
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
