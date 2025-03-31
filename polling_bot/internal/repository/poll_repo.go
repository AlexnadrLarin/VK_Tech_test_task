package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"polling_bot/internal/models"

	"github.com/tarantool/go-tarantool"
)

type PollRepository interface {
	SavePoll(ctx context.Context, poll models.Poll) error
	AddVoteAtomic(ctx context.Context, poll models.Poll) error
	GetPoll(ctx context.Context, id string) (models.Poll, error)
	ClosePoll(ctx context.Context, pollID string) error
	DeletePoll(ctx context.Context, id string) error
}

type TarantoolPollRepo struct {
	conn      *tarantool.Connection
	spaceName string
}

func NewTarantoolPollRepo(conn *tarantool.Connection, spaceName string) *TarantoolPollRepo {
	return &TarantoolPollRepo{
		conn:      conn,
		spaceName: spaceName,
	}
}

func (r *TarantoolPollRepo) SavePoll(ctx context.Context, poll models.Poll) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	// Порядок полей должен точно соответствовать структуре space в Tarantool
	data := []interface{}{
		poll.ID,       // field 1: id (string)
		poll.Creator,  // field 2: creator (string)
		poll.Question, // field 3: question (string)
		poll.Voters,   // field 4: voters (map)
		poll.Options,  // field 5: options (map)
		poll.Closed,   // field 6: is_closed (boolean)
	}

	_, err := r.conn.Replace(r.spaceName, data)
	if err != nil {
		return fmt.Errorf("ошибка сохранения опроса: %w", err)
	}
	return nil
}

func (r *TarantoolPollRepo) AddVoteAtomic(ctx context.Context, poll models.Poll) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	for i := 0; i < 5; i++ {
		_, err := r.conn.Update(r.spaceName, "primary", []interface{}{poll.ID}, []interface{}{
			[]interface{}{"=", 3, poll.Voters}, 
			[]interface{}{"=", 4, poll.Options}, 
		})

		if err == nil {
			return nil
		}

		if strings.Contains(err.Error(), "transaction conflict") {
			time.Sleep(time.Duration(i+1) * 20 * time.Millisecond)
			continue
		}
		return fmt.Errorf("ошибка сохранения голоса: %w", err)
	}
	return errors.New("не удалось сохранить голос после 5 попыток")
}


func (r *TarantoolPollRepo) GetPoll(ctx context.Context, id string) (models.Poll, error) {
	if err := ctx.Err(); err != nil {
		return models.Poll{}, err
	}

	res, err := r.conn.Select(r.spaceName, "primary", 0, 1, tarantool.IterEq, []interface{}{id})
	if err != nil {
		return models.Poll{}, fmt.Errorf("ошибка получения опроса: %w", err)
	}

	if len(res.Data) == 0 {
		return models.Poll{}, errors.New("опрос не найден")
	}

	return parsePollTuple(res.Data[0])
}

func (r *TarantoolPollRepo) ClosePoll(ctx context.Context, pollID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	_, err := r.conn.Update(r.spaceName, "primary", []interface{}{pollID}, []interface{}{
		[]interface{}{"=", 6, true},
	})
	if err != nil {
		return fmt.Errorf("ошибка закрытия опроса: %w", err)
	}
	return nil
}

func (r *TarantoolPollRepo) DeletePoll(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	_, err := r.conn.Delete(r.spaceName, "primary", []interface{}{id})
	if err != nil {
		return fmt.Errorf("ошибка удаления опроса: %w", err)
	}
	return nil
}

func parsePollTuple(data interface{}) (models.Poll, error) {
	tuple, ok := data.([]interface{})
	if !ok || len(tuple) < 6 {
		return models.Poll{}, errors.New("некорректный формат данных опроса")
	}

	poll := models.Poll{
		ID:       toString(tuple[0]), 
		Creator:  toString(tuple[1]), 
		Question: toString(tuple[2]), 
		Closed:   toBool(tuple[5]), 
	}

	if voters, ok := tuple[3].(map[interface{}]interface{}); ok {
		poll.Voters = make(map[string]bool, len(voters))
		for k, v := range voters {
			poll.Voters[toString(k)] = toBool(v)
		}
	}

	if opts, ok := tuple[4].(map[interface{}]interface{}); ok {
		poll.Options = make(map[string]int, len(opts))
		for k, v := range opts {
			poll.Options[toString(k)] = toInt(v)
		}
	}

	return poll, nil
}

func toString(val interface{}) string {
	if s, ok := val.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", val)
}

func toBool(val interface{}) bool {
	switch v := val.(type) {
	case bool:
		return v
	case int64:
		return v != 0
	case uint64:
		return v != 0
	case string:
		return strings.ToLower(v) == "true"
	default:
		return false
	}
}

func toInt(val interface{}) int {
	switch v := val.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case uint64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}

