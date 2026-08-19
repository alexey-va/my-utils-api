package health

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"time"

	"github.com/alexey-va/my-utils-api/internal/workout"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type StepsHistory struct {
	Days       []StepDay `json:"days"`
	TodaySteps *int      `json:"todaySteps"`
}

type WeightHistory struct {
	Days           []WeightDay `json:"days"`
	LatestWeightKg *float64    `json:"latestWeightKg"`
	LatestDate     *string     `json:"latestDate"`
}

type WeightResult struct {
	Date     string  `json:"date"`
	WeightKg float64 `json:"weightKg"`
	Created  bool    `json:"created"`
}

type Service struct{ pool *pgxpool.Pool }

func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

func (s *Service) UpsertSteps(ctx context.Context, parsed ParsedSteps) (int, error) {
	userID, err := s.localUserID(ctx)
	if err != nil {
		return 0, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	changed := 0
	for _, day := range parsed.Days {
		result, err := tx.Exec(ctx, `
			INSERT INTO health_steps(user_id,step_date,steps) VALUES($1::uuid,$2,$3)
			ON CONFLICT(user_id,step_date) DO UPDATE SET steps=excluded.steps,updated_at=now()
			WHERE health_steps.steps IS DISTINCT FROM excluded.steps
		`, userID, day.Date, day.Steps)
		if err != nil {
			return 0, err
		}
		changed += int(result.RowsAffected())
	}
	return changed, tx.Commit(ctx)
}

func (s *Service) StepsHistory(ctx context.Context, days int, today time.Time) (StepsHistory, error) {
	userID, err := s.localUserID(ctx)
	if err != nil {
		return StepsHistory{}, err
	}
	query := `SELECT step_date::text,steps FROM health_steps WHERE user_id=$1::uuid`
	arguments := []any{userID}
	if days > 0 {
		query += ` AND step_date BETWEEN $2 AND $3`
		arguments = append(arguments, today.AddDate(0, 0, -(days-1)), today)
	}
	query += ` ORDER BY step_date ASC`
	rows, err := s.pool.Query(ctx, query, arguments...)
	if err != nil {
		return StepsHistory{}, err
	}
	defer rows.Close()
	result := StepsHistory{Days: []StepDay{}}
	for rows.Next() {
		var day StepDay
		if err := rows.Scan(&day.Date, &day.Steps); err != nil {
			return StepsHistory{}, err
		}
		result.Days = append(result.Days, day)
		if day.Date == today.Format(time.DateOnly) {
			value := day.Steps
			result.TodaySteps = &value
		}
	}
	return result, rows.Err()
}

func (s *Service) UpsertWeight(ctx context.Context, weight float64, date string) (WeightResult, error) {
	normalized, err := normalizeWeight(weight)
	if err != nil {
		return WeightResult{}, err
	}
	if _, err := time.Parse(time.DateOnly, date); err != nil {
		return WeightResult{}, &workout.Error{Status: http.StatusBadRequest, Message: "Неверная дата date (YYYY-MM-DD)"}
	}
	userID, err := s.localUserID(ctx)
	if err != nil {
		return WeightResult{}, err
	}
	return upsertWeight(ctx, s.pool, userID, normalized, date)
}

func (s *Service) UpsertWeights(ctx context.Context, days []WeightDay) ([]WeightResult, error) {
	normalized := make([]WeightDay, len(days))
	for index, day := range days {
		value, err := normalizeWeight(day.WeightKg)
		if err != nil {
			return nil, err
		}
		if _, err := time.Parse(time.DateOnly, day.Date); err != nil {
			return nil, &workout.Error{Status: http.StatusBadRequest, Message: "Неверная дата date (YYYY-MM-DD)"}
		}
		normalized[index] = WeightDay{Date: day.Date, WeightKg: value}
	}
	userID, err := s.localUserID(ctx)
	if err != nil {
		return nil, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	result := make([]WeightResult, 0, len(normalized))
	for _, day := range normalized {
		value, err := upsertWeight(ctx, tx, userID, day.WeightKg, day.Date)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

type weightStore interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func upsertWeight(ctx context.Context, store weightStore, userID string, normalized float64, date string) (WeightResult, error) {
	var existed bool
	if err := store.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM health_body_weight WHERE user_id=$1::uuid AND weight_date=$2)`, userID, date).Scan(&existed); err != nil {
		return WeightResult{}, err
	}
	_, err := store.Exec(ctx, `
		INSERT INTO health_body_weight(user_id,weight_date,weight_kg) VALUES($1::uuid,$2,$3)
		ON CONFLICT(user_id,weight_date) DO UPDATE SET weight_kg=excluded.weight_kg,updated_at=now()
	`, userID, date, normalized)
	return WeightResult{Date: date, WeightKg: normalized, Created: !existed}, err
}

func (s *Service) WeightHistory(ctx context.Context, days int, today time.Time) (WeightHistory, error) {
	userID, err := s.localUserID(ctx)
	if err != nil {
		return WeightHistory{}, err
	}
	query := `SELECT weight_date::text,weight_kg::float8 FROM health_body_weight WHERE user_id=$1::uuid`
	arguments := []any{userID}
	if days > 0 {
		query += ` AND weight_date BETWEEN $2 AND $3`
		arguments = append(arguments, today.AddDate(0, 0, -(days-1)), today)
	}
	query += ` ORDER BY weight_date ASC`
	rows, err := s.pool.Query(ctx, query, arguments...)
	if err != nil {
		return WeightHistory{}, err
	}
	defer rows.Close()
	result := WeightHistory{Days: []WeightDay{}}
	for rows.Next() {
		var day WeightDay
		if err := rows.Scan(&day.Date, &day.WeightKg); err != nil {
			return WeightHistory{}, err
		}
		result.Days = append(result.Days, day)
	}
	if err := rows.Err(); err != nil {
		return WeightHistory{}, err
	}
	var latestDate string
	var latestWeight float64
	if err := s.pool.QueryRow(ctx, `SELECT weight_date::text,weight_kg::float8 FROM health_body_weight WHERE user_id=$1::uuid ORDER BY weight_date DESC LIMIT 1`, userID).Scan(&latestDate, &latestWeight); err == nil {
		result.LatestDate, result.LatestWeightKg = &latestDate, &latestWeight
	}
	return result, nil
}

func (s *Service) localUserID(ctx context.Context) (string, error) {
	var id string
	if err := s.pool.QueryRow(ctx, `SELECT id::text FROM users WHERE lower(email)=lower($1)`, workout.LocalWorkoutEmail).Scan(&id); err != nil {
		return "", fmt.Errorf("local workout user: %w", err)
	}
	return id, nil
}

func normalizeWeight(value float64) (float64, error) {
	normalized := math.Floor(value*10+0.5) / 10
	if normalized < 20 || normalized > 400 {
		return 0, &workout.Error{Status: http.StatusBadRequest, Message: "Вес тела должен быть от 20 до 400 кг"}
	}
	return normalized, nil
}
