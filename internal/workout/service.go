package workout

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const LocalWorkoutEmail = "local@workout"

type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string { return e.Message }

type Exercise struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	MuscleGroup string `json:"muscleGroup"`
}

type CreateExerciseRequest struct {
	Name        string  `json:"name"`
	MuscleGroup *string `json:"muscleGroup"`
}

type EntryRequest struct {
	ExerciseID  string  `json:"exerciseId"`
	PerformedOn string  `json:"performedOn"`
	WeightKg    float64 `json:"weightKg"`
	SetCount    int     `json:"setCount"`
	RepsPerSet  int     `json:"repsPerSet"`
	MaxReps     int     `json:"maxReps"`
	SetReps     []int   `json:"setReps"`
	SetWeights  []int   `json:"setWeights"`
}

type MoveRequest struct {
	FromExerciseID string `json:"fromExerciseId"`
	FromDate       string `json:"fromDate"`
	ToExerciseID   string `json:"toExerciseId"`
	ToDate         string `json:"toDate"`
}

type Cell struct {
	WeightKg   float64 `json:"weightKg"`
	SetCount   int     `json:"setCount"`
	RepsPerSet int     `json:"repsPerSet"`
	MaxReps    int     `json:"maxReps"`
	SetReps    []int   `json:"setReps"`
	Display    string  `json:"display"`
}

type GridRow struct {
	ExerciseID   string          `json:"exerciseId"`
	ExerciseName string          `json:"exerciseName"`
	Cells        map[string]Cell `json:"cells"`
}

type Grid struct {
	Dates []string  `json:"dates"`
	Rows  []GridRow `json:"rows"`
}

type ProgressPoint struct {
	Date       string  `json:"date"`
	WeightKg   float64 `json:"weightKg"`
	SetCount   int     `json:"setCount"`
	RepsPerSet int     `json:"repsPerSet"`
	MaxReps    int     `json:"maxReps"`
	SetReps    []int   `json:"setReps"`
	Volume     float64 `json:"volume"`
}

type Stats struct {
	Sessions       int      `json:"sessions"`
	BestWeightKg   *float64 `json:"bestWeightKg"`
	LatestWeightKg *float64 `json:"latestWeightKg"`
	BestMaxReps    *int     `json:"bestMaxReps"`
	BestVolume     *float64 `json:"bestVolume"`
}

type Progress struct {
	Exercise Exercise        `json:"exercise"`
	Points   []ProgressPoint `json:"points"`
	Stats    Stats           `json:"stats"`
}

type Service struct{ pool *pgxpool.Pool }

func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

func (s *Service) ListExercises(ctx context.Context) ([]Exercise, error) {
	userID, err := s.localUserID(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT id::text, name, muscle_group FROM exercises WHERE user_id = $1::uuid ORDER BY name ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Exercise, 0)
	for rows.Next() {
		var exercise Exercise
		if err := rows.Scan(&exercise.ID, &exercise.Name, &exercise.MuscleGroup); err != nil {
			return nil, err
		}
		result = append(result, exercise)
	}
	return result, rows.Err()
}

func (s *Service) CreateExercise(ctx context.Context, request CreateExerciseRequest) (Exercise, error) {
	userID, err := s.localUserID(ctx)
	if err != nil {
		return Exercise{}, err
	}
	name := strings.TrimSpace(request.Name)
	if name == "" {
		return Exercise{}, badRequest("Exercise name is required")
	}
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM exercises WHERE user_id=$1::uuid AND lower(name)=lower($2))`, userID, name).Scan(&exists); err != nil {
		return Exercise{}, err
	}
	if exists {
		return Exercise{}, conflict("Exercise already exists")
	}
	group := normalizeMuscleGroup(request.MuscleGroup)
	var result Exercise
	err = s.pool.QueryRow(ctx, `INSERT INTO exercises(user_id,name,muscle_group) VALUES($1::uuid,$2,$3) RETURNING id::text,name,muscle_group`, userID, name, group).Scan(&result.ID, &result.Name, &result.MuscleGroup)
	return result, err
}

func (s *Service) UpdateExercise(ctx context.Context, id string, request CreateExerciseRequest) (Exercise, error) {
	userID, err := s.localUserID(ctx)
	if err != nil {
		return Exercise{}, err
	}
	current, err := s.ownedExercise(ctx, userID, id)
	if err != nil {
		return Exercise{}, err
	}
	name := strings.TrimSpace(request.Name)
	if name == "" {
		return Exercise{}, badRequest("Exercise name is required")
	}
	var duplicate bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM exercises WHERE user_id=$1::uuid AND lower(name)=lower($2) AND id<>$3::uuid)`, userID, name, id).Scan(&duplicate); err != nil {
		return Exercise{}, err
	}
	if duplicate {
		return Exercise{}, conflict("Exercise already exists")
	}
	group := current.MuscleGroup
	if request.MuscleGroup != nil {
		group = normalizeMuscleGroup(request.MuscleGroup)
	}
	var result Exercise
	err = s.pool.QueryRow(ctx, `UPDATE exercises SET name=$2,muscle_group=$3 WHERE id=$1::uuid RETURNING id::text,name,muscle_group`, id, name, group).Scan(&result.ID, &result.Name, &result.MuscleGroup)
	return result, err
}

func (s *Service) DeleteExercise(ctx context.Context, id string) error {
	userID, err := s.localUserID(ctx)
	if err != nil {
		return err
	}
	if _, err := s.ownedExercise(ctx, userID, id); err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `DELETE FROM exercises WHERE id=$1::uuid`, id)
	return err
}

type entry struct {
	ExerciseID, ExerciseName, Date string
	Weight                         float64
	SetCount, RepsPerSet, MaxReps  int
	SetReps, SetWeights            string
}

func (e entry) reps() []int { return EffectiveReps(e.SetCount, e.RepsPerSet, e.MaxReps, e.SetReps) }

func (s *Service) Grid(ctx context.Context) (Grid, error) {
	exercises, err := s.ListExercises(ctx)
	if err != nil {
		return Grid{}, err
	}
	entries, err := s.entries(ctx, "", false)
	if err != nil {
		return Grid{}, err
	}
	dates := make([]string, 0)
	seen := make(map[string]struct{})
	cells := make(map[string]map[string]Cell)
	for _, item := range entries {
		if _, ok := seen[item.Date]; !ok {
			seen[item.Date] = struct{}{}
			dates = append(dates, item.Date)
		}
		if cells[item.ExerciseID] == nil {
			cells[item.ExerciseID] = make(map[string]Cell)
		}
		reps := item.reps()
		weights := ParseStorage(item.SetWeights)
		var exposed []int
		if item.SetReps != "" {
			exposed = ParseStorage(item.SetReps)
		}
		cells[item.ExerciseID][item.Date] = Cell{
			WeightKg: item.Weight, SetCount: item.SetCount, RepsPerSet: item.RepsPerSet,
			MaxReps: item.MaxReps, SetReps: exposed, Display: Display(item.Weight, reps, weights),
		}
	}
	rows := make([]GridRow, 0, len(exercises))
	for _, exercise := range exercises {
		rowCells := cells[exercise.ID]
		if rowCells == nil {
			rowCells = map[string]Cell{}
		}
		rows = append(rows, GridRow{ExerciseID: exercise.ID, ExerciseName: exercise.Name, Cells: rowCells})
	}
	return Grid{Dates: dates, Rows: rows}, nil
}

func (s *Service) Progress(ctx context.Context, exerciseID string) (Progress, error) {
	userID, err := s.localUserID(ctx)
	if err != nil {
		return Progress{}, err
	}
	exercise, err := s.ownedExercise(ctx, userID, exerciseID)
	if err != nil {
		return Progress{}, err
	}
	entries, err := s.entries(ctx, exerciseID, true)
	if err != nil {
		return Progress{}, err
	}
	points := make([]ProgressPoint, 0, len(entries))
	for _, item := range entries {
		reps := item.reps()
		weights := ParseStorage(item.SetWeights)
		var exposed []int
		if item.SetReps != "" {
			exposed = ParseStorage(item.SetReps)
		}
		points = append(points, ProgressPoint{
			Date: item.Date, WeightKg: item.Weight, SetCount: item.SetCount, RepsPerSet: item.RepsPerSet,
			MaxReps: item.MaxReps, SetReps: exposed, Volume: Volume(item.Weight, reps, weights),
		})
	}
	return Progress{Exercise: exercise, Points: points, Stats: stats(points)}, nil
}

func (s *Service) UpsertEntry(ctx context.Context, request EntryRequest) error {
	if request.WeightKg < 0.25 {
		return badRequest("weightKg must be at least 0.25")
	}
	date, err := time.Parse(time.DateOnly, request.PerformedOn)
	if err != nil {
		return badRequest("performedOn must be YYYY-MM-DD")
	}
	normalized, err := NormalizeSets(request.SetCount, request.RepsPerSet, request.MaxReps, request.SetReps, request.SetWeights)
	if err != nil {
		return badRequest(err.Error())
	}
	userID, err := s.localUserID(ctx)
	if err != nil {
		return err
	}
	if _, err := s.ownedExercise(ctx, userID, request.ExerciseID); err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO workout_entries(user_id,exercise_id,performed_on,weight_kg,set_count,reps_per_set,max_reps,set_reps,set_weights)
		VALUES($1::uuid,$2::uuid,$3,$4,$5,$6,$7,NULLIF($8,''),NULLIF($9,''))
		ON CONFLICT(user_id,exercise_id,performed_on) DO UPDATE SET
			weight_kg=excluded.weight_kg,set_count=excluded.set_count,reps_per_set=excluded.reps_per_set,
			max_reps=excluded.max_reps,set_reps=excluded.set_reps,set_weights=excluded.set_weights
	`, userID, request.ExerciseID, date, request.WeightKg, normalized.SetCount, normalized.RepsPerSet, normalized.MaxReps, normalized.RepsStorage, normalized.WeightsStorage)
	return err
}

func (s *Service) DeleteEntry(ctx context.Context, exerciseID, dateText string) error {
	date, err := time.Parse(time.DateOnly, dateText)
	if err != nil {
		return badRequest("performedOn must be YYYY-MM-DD")
	}
	userID, err := s.localUserID(ctx)
	if err != nil {
		return err
	}
	exercise, err := s.ownedExercise(ctx, userID, exerciseID)
	if err != nil {
		return err
	}
	result, err := s.pool.Exec(ctx, `DELETE FROM workout_entries WHERE user_id=$1::uuid AND exercise_id=$2::uuid AND performed_on=$3`, userID, exerciseID, date)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return notFound(fmt.Sprintf("No workout entry for %s on %s", exercise.Name, dateText))
	}
	return nil
}

func (s *Service) MoveEntry(ctx context.Context, request MoveRequest) error {
	if request.FromExerciseID == request.ToExerciseID && request.FromDate == request.ToDate {
		return nil
	}
	fromDate, fromErr := time.Parse(time.DateOnly, request.FromDate)
	toDate, toErr := time.Parse(time.DateOnly, request.ToDate)
	if fromErr != nil || toErr != nil {
		return badRequest("dates must be YYYY-MM-DD")
	}
	userID, err := s.localUserID(ctx)
	if err != nil {
		return err
	}
	if _, err := s.ownedExercise(ctx, userID, request.FromExerciseID); err != nil {
		return err
	}
	if _, err := s.ownedExercise(ctx, userID, request.ToExerciseID); err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	var sourceID string
	if err := tx.QueryRow(ctx, `SELECT id::text FROM workout_entries WHERE user_id=$1::uuid AND exercise_id=$2::uuid AND performed_on=$3 FOR UPDATE`, userID, request.FromExerciseID, fromDate).Scan(&sourceID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return notFound("Source workout entry not found")
		}
		return err
	}
	var target bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM workout_entries WHERE user_id=$1::uuid AND exercise_id=$2::uuid AND performed_on=$3)`, userID, request.ToExerciseID, toDate).Scan(&target); err != nil {
		return err
	}
	if target {
		return conflict("Target workout cell is not empty")
	}
	if _, err := tx.Exec(ctx, `UPDATE workout_entries SET exercise_id=$2::uuid,performed_on=$3 WHERE id=$1::uuid`, sourceID, request.ToExerciseID, toDate); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Service) entries(ctx context.Context, exerciseID string, ascending bool) ([]entry, error) {
	userID, err := s.localUserID(ctx)
	if err != nil {
		return nil, err
	}
	order := "w.performed_on DESC, w.created_at DESC"
	if ascending {
		order = "w.performed_on ASC"
	}
	query := `SELECT e.id::text,e.name,w.performed_on::text,w.weight_kg,w.set_count,w.reps_per_set,w.max_reps,COALESCE(w.set_reps,''),COALESCE(w.set_weights,'') FROM workout_entries w JOIN exercises e ON e.id=w.exercise_id WHERE w.user_id=$1::uuid`
	arguments := []any{userID}
	if exerciseID != "" {
		query += ` AND w.exercise_id=$2::uuid`
		arguments = append(arguments, exerciseID)
	}
	query += ` ORDER BY ` + order
	rows, err := s.pool.Query(ctx, query, arguments...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]entry, 0)
	for rows.Next() {
		var item entry
		if err := rows.Scan(&item.ExerciseID, &item.ExerciseName, &item.Date, &item.Weight, &item.SetCount, &item.RepsPerSet, &item.MaxReps, &item.SetReps, &item.SetWeights); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Service) localUserID(ctx context.Context) (string, error) {
	var id string
	if err := s.pool.QueryRow(ctx, `SELECT id::text FROM users WHERE lower(email)=lower($1)`, LocalWorkoutEmail).Scan(&id); err != nil {
		return "", &Error{Status: http.StatusInternalServerError, Message: "Local workout user is not configured"}
	}
	return id, nil
}

func (s *Service) ownedExercise(ctx context.Context, userID, id string) (Exercise, error) {
	var exercise Exercise
	if err := s.pool.QueryRow(ctx, `SELECT id::text,name,muscle_group FROM exercises WHERE id=$1::uuid AND user_id=$2::uuid`, id, userID).Scan(&exercise.ID, &exercise.Name, &exercise.MuscleGroup); err != nil {
		return Exercise{}, notFound("Exercise not found")
	}
	return exercise, nil
}

func stats(points []ProgressPoint) Stats {
	result := Stats{Sessions: len(points)}
	if len(points) == 0 {
		return result
	}
	bestWeight, latestWeight := points[0].WeightKg, points[len(points)-1].WeightKg
	bestMax, bestVolume := points[0].MaxReps, points[0].Volume
	for _, point := range points[1:] {
		bestWeight = max(bestWeight, point.WeightKg)
		bestMax = max(bestMax, point.MaxReps)
		bestVolume = max(bestVolume, point.Volume)
	}
	result.BestWeightKg, result.LatestWeightKg = &bestWeight, &latestWeight
	result.BestMaxReps, result.BestVolume = &bestMax, &bestVolume
	return result
}

func normalizeMuscleGroup(raw *string) string {
	if raw == nil {
		return "other"
	}
	value := strings.ToLower(strings.TrimSpace(*raw))
	allowed := []string{"arms", "back", "chest", "core", "legs", "other", "shoulders"}
	if _, ok := sort.Find(len(allowed), func(index int) int { return strings.Compare(allowed[index], value) }); ok {
		return value
	}
	return "other"
}

func badRequest(message string) error { return &Error{Status: http.StatusBadRequest, Message: message} }
func conflict(message string) error   { return &Error{Status: http.StatusConflict, Message: message} }
func notFound(message string) error   { return &Error{Status: http.StatusNotFound, Message: message} }
