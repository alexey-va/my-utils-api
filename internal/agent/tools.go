package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/alexey-va/my-utils-api/internal/health"
	"github.com/alexey-va/my-utils-api/internal/workout"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const sandboxIDMinimum int64 = -9_000_000_000_000_000
const sandboxIDMaximum int64 = -8_000_000_000_000_000

type NotificationScheduler interface {
	SendNow(context.Context, int64, string) (string, error)
	Schedule(context.Context, int64, string, string) (string, error)
	Cancel(context.Context, string) (bool, error)
}

type ToolDelivery interface {
	SendRichMessage(context.Context, int64, string, string) error
	SendProgressChart(context.Context, int64, string, int) error
	SendOneRM(context.Context, int64, string, float64, int, float64) error
}

type ToolService struct {
	pool      *pgxpool.Pool
	workout   *workout.Service
	health    *health.Service
	memory    *Memory
	scheduler NotificationScheduler
	delivery  ToolDelivery
	now       func() time.Time
	zoneID    func() string
	metrics   MetricsRecorder
}

func (s *ToolService) SetMetrics(metrics MetricsRecorder) { s.metrics = metrics }
func (s *ToolService) SetZoneID(zoneID func() string)     { s.zoneID = zoneID }

func NewToolService(pool *pgxpool.Pool, workouts *workout.Service, healthService *health.Service, memory *Memory, scheduler NotificationScheduler, delivery ToolDelivery) *ToolService {
	return &ToolService{pool: pool, workout: workouts, health: healthService, memory: memory, scheduler: scheduler, delivery: delivery, now: time.Now}
}

func (s *ToolService) Execute(ctx context.Context, chatID int64, name string, args map[string]any, _ string, sandbox bool) (result string, err error) {
	started := time.Now()
	path := metricsPath(ctx, sandbox)
	defer func() {
		if s.metrics != nil {
			status := "success"
			if err != nil {
				status = "error"
			}
			s.metrics.RecordTool(path, NormalizeToolName(name), status, time.Since(started))
		}
	}()
	defer func() {
		if recovered := recover(); recovered != nil {
			if argument, ok := recovered.(argumentPanic); ok {
				result, err = "", argument
				return
			}
			panic(recovered)
		}
	}()
	name = NormalizeToolName(name)
	if isSandboxID(chatID) {
		if !sandbox {
			return "", errors.New("reserved sandbox chat cannot execute real tools")
		}
		return s.executeSandbox(ctx, chatID, name, args)
	}
	if sandbox {
		return "", errors.New("sandbox execution requires a reserved test chat ID")
	}
	return s.executeReal(ctx, chatID, name, args)
}

func (s *ToolService) executeReal(ctx context.Context, chatID int64, name string, args map[string]any) (string, error) {
	switch name {
	case "list_exercises":
		exercises, err := s.workout.ListExercises(ctx)
		if err != nil {
			return "", err
		}
		if len(exercises) == 0 {
			return "Упражнений пока нет.", nil
		}
		lines := make([]string, len(exercises))
		for index, exercise := range exercises {
			lines[index] = fmt.Sprintf("• %s (%s) [id=%s]", exercise.Name, exercise.MuscleGroup, exercise.ID)
		}
		return strings.Join(lines, "\n"), nil
	case "create_exercise":
		group := optionalString(args, "muscle_group")
		var groupPointer *string
		if group != "" {
			groupPointer = &group
		}
		exercise, err := s.workout.CreateExercise(ctx, workout.CreateExerciseRequest{Name: requiredString(args, "name"), MuscleGroup: groupPointer})
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Создано упражнение «%s» (%s).", exercise.Name, exercise.MuscleGroup), nil
	case "rename_exercise":
		exercise, err := s.findExercise(ctx, requiredString(args, "current_name"))
		if err != nil {
			return "", err
		}
		group := optionalString(args, "muscle_group")
		var groupPointer *string
		if group != "" {
			groupPointer = &group
		}
		updated, err := s.workout.UpdateExercise(ctx, exercise.ID, workout.CreateExerciseRequest{Name: requiredString(args, "new_name"), MuscleGroup: groupPointer})
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("«%s» переименовано в «%s».", exercise.Name, updated.Name), nil
	case "log_workout":
		exercise, err := s.findExercise(ctx, requiredString(args, "exercise_name"))
		if err != nil {
			return "", err
		}
		parsed, err := workout.ParseNotation(requiredString(args, "notation"))
		if err != nil {
			return "", err
		}
		date := optionalString(args, "date")
		if date == "" {
			date = optionalString(args, "performed_on")
		}
		if date == "" {
			date = s.today()
		}
		err = s.workout.UpsertEntry(ctx, workout.EntryRequest{ExerciseID: exercise.ID, PerformedOn: date, WeightKg: parsed.WeightKg, SetCount: parsed.SetCount, RepsPerSet: parsed.RepsPerSet, MaxReps: parsed.MaxReps, SetReps: parsed.Reps, SetWeights: parsed.Weights})
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Записано: %s, %s — %s.", exercise.Name, date, workout.Display(parsed.WeightKg, parsed.Reps, parsed.Weights)), nil
	case "delete_workout":
		exercise, err := s.findExercise(ctx, requiredString(args, "exercise_name"))
		if err != nil {
			return "", err
		}
		date := optionalString(args, "performed_on")
		if date == "" {
			date = optionalString(args, "date")
		}
		if date == "" {
			date = s.today()
		}
		if err := s.workout.DeleteEntry(ctx, exercise.ID, date); err != nil {
			return "", err
		}
		return fmt.Sprintf("Удалена запись %s за %s.", exercise.Name, date), nil
	case "get_progress", "get_exercise_progresses":
		nameArg := optionalString(args, "exercise")
		if nameArg == "" {
			nameArg = requiredString(args, "exercises")
		}
		exercise, err := s.findExercise(ctx, nameArg)
		if err != nil {
			return "", err
		}
		progress, err := s.workout.Progress(ctx, exercise.ID)
		if err != nil {
			return "", err
		}
		limit := optionalInt(args, "recent_sessions", 6, 1, 20)
		points := progress.Points
		if len(points) > limit {
			points = points[len(points)-limit:]
		}
		if len(points) == 0 {
			return fmt.Sprintf("«%s» — записей пока нет.", exercise.Name), nil
		}
		lines := []string{fmt.Sprintf("«%s» — прогресс:", exercise.Name)}
		for _, point := range points {
			lines = append(lines, fmt.Sprintf("• %s: %s", point.Date, workout.Display(point.WeightKg, point.SetReps, nil)))
		}
		return strings.Join(lines, "\n"), nil
	case "get_days", "get_day_summaries":
		grid, err := s.workout.Grid(ctx)
		if err != nil {
			return "", err
		}
		dates, err := resolveDates(args, s.today())
		if err != nil {
			return "", err
		}
		return formatGridDays(grid, dates), nil
	case "log_body_weight":
		weight, err := requiredFloat(args, "weight_kg")
		if err != nil {
			return "", err
		}
		date := optionalString(args, "date")
		if date == "" {
			date = optionalString(args, "performed_on")
		}
		if date == "" {
			date = s.today()
		}
		result, err := s.health.UpsertWeight(ctx, weight, date)
		if err != nil {
			return "", err
		}
		verb := "обновлён"
		if result.Created {
			verb = "сохранён"
		}
		return fmt.Sprintf("Вес %.1f кг %s за %s.", result.WeightKg, verb, result.Date), nil
	case "get_body_weight":
		days := optionalInt(args, "recent_days", 14, 1, 90)
		history, err := s.health.WeightHistory(ctx, days, s.todayTime())
		if err != nil {
			return "", err
		}
		if len(history.Days) == 0 {
			return "Замеров веса пока нет.", nil
		}
		lines := make([]string, 0, len(history.Days))
		for index := len(history.Days) - 1; index >= 0; index-- {
			lines = append(lines, fmt.Sprintf("• %s: %.1f кг", history.Days[index].Date, history.Days[index].WeightKg))
		}
		return strings.Join(lines, "\n"), nil
	case "remember_fact":
		fact, err := s.memory.CreateFact(ctx, chatID, requiredString(args, "content"), nil)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Факт сохранён [%s].", fact.ID), nil
	case "forget_fact":
		if err := s.memory.DeleteFact(ctx, requiredString(args, "fact_id")); err != nil {
			return "", err
		}
		return "Факт удалён.", nil
	case "manage_user_fact":
		action := strings.ToLower(requiredString(args, "action"))
		switch action {
		case "remember":
			return s.executeReal(ctx, chatID, "remember_fact", args)
		case "forget":
			return s.executeReal(ctx, chatID, "forget_fact", args)
		case "update":
			fact, err := s.memory.UpdateFact(ctx, requiredString(args, "fact_id"), requiredString(args, "content"), nil)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("Факт обновлён [%s].", fact.ID), nil
		default:
			return "", fmt.Errorf("неизвестный action %q", action)
		}
	case "send_notification":
		if s.scheduler == nil {
			return "", errors.New("Temporal выключен — уведомление не отправлено")
		}
		return s.scheduler.SendNow(ctx, chatID, requiredString(args, "message"))
	case "schedule_notification":
		if s.scheduler == nil {
			return "", errors.New("Temporal выключен — напоминание не запланировано")
		}
		return s.scheduler.Schedule(ctx, chatID, requiredString(args, "message"), requiredString(args, "deliver_at"))
	case "cancel_notification":
		if s.scheduler == nil {
			return "", errors.New("Temporal выключен")
		}
		workflowID := requiredString(args, "workflow_id")
		cancelled, err := s.scheduler.Cancel(ctx, workflowID)
		if err != nil {
			return "", err
		}
		if !cancelled {
			return "Workflow не найден: " + workflowID, nil
		}
		return "Напоминание отменено (" + workflowID + ").", nil
	case "send_rich_message":
		if s.delivery == nil {
			return "", errors.New("Telegram недоступен")
		}
		text := NormalizeReply(requiredString(args, "text"))
		if err := s.delivery.SendRichMessage(ctx, chatID, text, optionalString(args, "buttons")); err != nil {
			return "", err
		}
		return "Сообщение отправлено.", nil
	case "send_progress_chart":
		if s.delivery == nil {
			return "", errors.New("Telegram недоступен")
		}
		if err := s.delivery.SendProgressChart(ctx, chatID, requiredString(args, "exercise_name"), optionalInt(args, "recent_sessions", 12, 1, 20)); err != nil {
			return "", err
		}
		return "График прогресса отправлен в чат.", nil
	case "estimate_1rm":
		exercise, err := s.findExercise(ctx, requiredString(args, "exercise_name"))
		if err != nil {
			return "", err
		}
		progress, err := s.workout.Progress(ctx, exercise.ID)
		if err != nil {
			return "", err
		}
		date := optionalString(args, "date")
		var point *workout.ProgressPoint
		for index := len(progress.Points) - 1; index >= 0; index-- {
			if date == "" || progress.Points[index].Date == date {
				copy := progress.Points[index]
				point = &copy
				break
			}
		}
		if point == nil {
			return "", fmt.Errorf("нет записей по %s", exercise.Name)
		}
		reps := point.MaxReps
		estimate := point.WeightKg * (1 + float64(reps)/30)
		if s.delivery != nil {
			if err := s.delivery.SendOneRM(ctx, chatID, exercise.Name, point.WeightKg, reps, estimate); err != nil {
				return "", err
			}
		}
		return fmt.Sprintf("Оценка 1ПМ для «%s»: %.1f кг (Эпли, %.1f кг × %d).", exercise.Name, estimate, point.WeightKg, reps), nil
	default:
		return "", fmt.Errorf("неизвестный инструмент: %s", name)
	}
}

type sandboxState struct {
	Exercises     []sandboxExercise     `json:"exercises"`
	Workouts      []sandboxWorkout      `json:"workouts"`
	BodyWeights   []sandboxWeight       `json:"bodyWeights"`
	Facts         []sandboxFact         `json:"facts"`
	Notifications []sandboxNotification `json:"notifications"`
}
type sandboxExercise struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	MuscleGroup string `json:"muscleGroup"`
}
type sandboxWorkout struct {
	ExerciseID   string  `json:"exerciseId"`
	ExerciseName string  `json:"exerciseName"`
	PerformedOn  string  `json:"performedOn"`
	WeightKg     float64 `json:"weightKg"`
	Reps         []int   `json:"reps"`
	Weights      []int   `json:"weights"`
}
type sandboxWeight struct {
	PerformedOn string  `json:"performedOn"`
	WeightKg    float64 `json:"weightKg"`
}
type sandboxFact struct {
	ID      string `json:"id"`
	Content string `json:"content"`
}
type sandboxNotification struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

func (s *ToolService) executeSandbox(ctx context.Context, chatID int64, name string, args map[string]any) (string, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	var raw string
	if err := tx.QueryRow(ctx, `SELECT state_json FROM agent_test_sandbox_states WHERE memory_chat_id=$1 FOR UPDATE`, chatID).Scan(&raw); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", errors.New("sandbox state not found; refusing real-data fallback")
		}
		return "", err
	}
	state := sandboxState{}
	if strings.TrimSpace(raw) != "" && raw != "{}" {
		if err := json.Unmarshal([]byte(raw), &state); err != nil {
			return "", fmt.Errorf("decode sandbox state: %w", err)
		}
	}
	result, err := s.runSandboxTool(&state, name, args)
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `UPDATE agent_test_sandbox_states SET state_json=$2,version=version+1,updated_at=now() WHERE memory_chat_id=$1`, chatID, string(encoded)); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return result, nil
}

func (s *ToolService) runSandboxTool(state *sandboxState, name string, args map[string]any) (string, error) {
	switch name {
	case "list_exercises":
		if len(state.Exercises) == 0 {
			return "В SANDBOX упражнений пока нет.", nil
		}
		lines := []string{}
		for _, exercise := range state.Exercises {
			lines = append(lines, fmt.Sprintf("• %s (%s) [sandbox_id=%s]", exercise.Name, exercise.MuscleGroup, exercise.ID))
		}
		sort.Strings(lines)
		return "SANDBOX:\n" + strings.Join(lines, "\n"), nil
	case "create_exercise":
		nameArg := requiredString(args, "name")
		for _, exercise := range state.Exercises {
			if strings.EqualFold(exercise.Name, nameArg) {
				return "", fmt.Errorf("в SANDBOX уже есть упражнение %q", nameArg)
			}
		}
		group := optionalString(args, "muscle_group")
		if group == "" {
			group = "other"
		}
		state.Exercises = append(state.Exercises, sandboxExercise{ID: randomID("exercise"), Name: nameArg, MuscleGroup: group})
		return fmt.Sprintf("SANDBOX: создано упражнение «%s» (%s).", nameArg, group), nil
	case "rename_exercise":
		exercise, err := sandboxExerciseByName(state, requiredString(args, "current_name"))
		if err != nil {
			return "", err
		}
		previous := exercise.Name
		exercise.Name = requiredString(args, "new_name")
		if group := optionalString(args, "muscle_group"); group != "" {
			exercise.MuscleGroup = group
		}
		for index := range state.Workouts {
			if state.Workouts[index].ExerciseID == exercise.ID {
				state.Workouts[index].ExerciseName = exercise.Name
			}
		}
		return fmt.Sprintf("SANDBOX: «%s» переименовано в «%s».", previous, exercise.Name), nil
	case "log_workout":
		exercise, err := sandboxExerciseByName(state, requiredString(args, "exercise_name"))
		if err != nil {
			return "", err
		}
		parsed, err := workout.ParseNotation(requiredString(args, "notation"))
		if err != nil {
			return "", err
		}
		date := optionalString(args, "date")
		if date == "" {
			date = optionalString(args, "performed_on")
		}
		if date == "" {
			date = s.today()
		}
		filtered := state.Workouts[:0]
		for _, row := range state.Workouts {
			if row.ExerciseID != exercise.ID || row.PerformedOn != date {
				filtered = append(filtered, row)
			}
		}
		state.Workouts = append(filtered, sandboxWorkout{ExerciseID: exercise.ID, ExerciseName: exercise.Name, PerformedOn: date, WeightKg: parsed.WeightKg, Reps: parsed.Reps, Weights: parsed.Weights})
		return fmt.Sprintf("SANDBOX: записано %s, %s — %s", exercise.Name, date, workout.Display(parsed.WeightKg, parsed.Reps, parsed.Weights)), nil
	case "delete_workout":
		exercise, err := sandboxExerciseByName(state, requiredString(args, "exercise_name"))
		if err != nil {
			return "", err
		}
		date := optionalString(args, "performed_on")
		if date == "" {
			date = s.today()
		}
		removed := false
		filtered := state.Workouts[:0]
		for _, row := range state.Workouts {
			if row.ExerciseID == exercise.ID && row.PerformedOn == date {
				removed = true
				continue
			}
			filtered = append(filtered, row)
		}
		if !removed {
			return "", fmt.Errorf("в SANDBOX нет записи за %s", date)
		}
		state.Workouts = filtered
		return fmt.Sprintf("SANDBOX: удалена запись %s за %s.", exercise.Name, date), nil
	case "get_progress", "get_exercise_progresses":
		exerciseName := optionalString(args, "exercise")
		if exerciseName == "" {
			exerciseName = requiredString(args, "exercises")
		}
		exercise, err := sandboxExerciseByName(state, exerciseName)
		if err != nil {
			return "", err
		}
		lines := []string{fmt.Sprintf("«%s» — SANDBOX-прогресс:", exercise.Name)}
		for _, row := range state.Workouts {
			if row.ExerciseID == exercise.ID {
				lines = append(lines, fmt.Sprintf("• %s: %s", row.PerformedOn, workout.Display(row.WeightKg, row.Reps, row.Weights)))
			}
		}
		if len(lines) == 1 {
			return fmt.Sprintf("«%s» — в SANDBOX записей пока нет.", exercise.Name), nil
		}
		return strings.Join(lines, "\n"), nil
	case "get_days", "get_day_summaries":
		dates, err := resolveDates(args, s.today())
		if err != nil {
			return "", err
		}
		blocks := []string{}
		for _, date := range dates {
			lines := []string{"SANDBOX-тренировка за " + date + ":"}
			for _, row := range state.Workouts {
				if row.PerformedOn == date {
					lines = append(lines, fmt.Sprintf("• %s: %s", row.ExerciseName, workout.Display(row.WeightKg, row.Reps, row.Weights)))
				}
			}
			if len(lines) == 1 {
				blocks = append(blocks, "За "+date+" в SANDBOX записей нет.")
			} else {
				blocks = append(blocks, strings.Join(lines, "\n"))
			}
		}
		return strings.Join(blocks, "\n\n"), nil
	case "log_body_weight":
		value, err := requiredFloat(args, "weight_kg")
		if err != nil {
			return "", err
		}
		date := optionalString(args, "date")
		if date == "" {
			date = s.today()
		}
		filtered := state.BodyWeights[:0]
		for _, row := range state.BodyWeights {
			if row.PerformedOn != date {
				filtered = append(filtered, row)
			}
		}
		state.BodyWeights = append(filtered, sandboxWeight{PerformedOn: date, WeightKg: value})
		return fmt.Sprintf("SANDBOX: вес %.1f кг сохранён за %s.", value, date), nil
	case "get_body_weight":
		if len(state.BodyWeights) == 0 {
			return "В SANDBOX замеров веса пока нет.", nil
		}
		lines := []string{}
		for _, row := range state.BodyWeights {
			lines = append(lines, fmt.Sprintf("• %s: %.1f кг", row.PerformedOn, row.WeightKg))
		}
		return strings.Join(lines, "\n"), nil
	case "remember_fact":
		fact := sandboxFact{ID: randomID("fact"), Content: requiredString(args, "content")}
		state.Facts = append(state.Facts, fact)
		return fmt.Sprintf("SANDBOX: факт сохранён [%s].", fact.ID), nil
	case "forget_fact":
		return sandboxForgetFact(state, requiredString(args, "fact_id"))
	case "manage_user_fact":
		switch strings.ToLower(requiredString(args, "action")) {
		case "remember":
			return s.runSandboxTool(state, "remember_fact", args)
		case "forget":
			return sandboxForgetFact(state, requiredString(args, "fact_id"))
		case "update":
			for index := range state.Facts {
				if state.Facts[index].ID == requiredString(args, "fact_id") {
					state.Facts[index].Content = requiredString(args, "content")
					return "SANDBOX: факт обновлён.", nil
				}
			}
			return "", errors.New("sandbox-факт не найден")
		default:
			return "", errors.New("неизвестный action")
		}
	case "send_notification", "schedule_notification":
		status := "sent"
		if name == "schedule_notification" {
			status = "scheduled:" + requiredString(args, "deliver_at")
		}
		notification := sandboxNotification{ID: randomID("sandbox"), Status: status, Message: requiredString(args, "message")}
		state.Notifications = append(state.Notifications, notification)
		return fmt.Sprintf("SANDBOX: уведомление %s сохранено локально; наружу ничего не отправлено.", notification.ID), nil
	case "cancel_notification":
		id := requiredString(args, "workflow_id")
		for index := range state.Notifications {
			if state.Notifications[index].ID == id {
				state.Notifications[index].Status = "cancelled"
				return "SANDBOX: уведомление " + id + " отменено локально.", nil
			}
		}
		return "", errors.New("sandbox-уведомление не найдено")
	case "send_rich_message":
		return "SANDBOX: сообщение сохранено только как tool result; в Telegram ничего не отправлено.", nil
	case "send_progress_chart":
		progress, err := s.runSandboxTool(state, "get_progress", map[string]any{"exercise": requiredString(args, "exercise_name")})
		if err != nil {
			return "", err
		}
		return "SANDBOX: график не отправлялся наружу.\n" + progress, nil
	case "estimate_1rm":
		exercise, err := sandboxExerciseByName(state, requiredString(args, "exercise_name"))
		if err != nil {
			return "", err
		}
		var latest *sandboxWorkout
		for index := range state.Workouts {
			row := &state.Workouts[index]
			if row.ExerciseID == exercise.ID && (latest == nil || row.PerformedOn > latest.PerformedOn) {
				latest = row
			}
		}
		if latest == nil {
			return "", errors.New("в SANDBOX нет записей по упражнению")
		}
		reps := 1
		for _, value := range latest.Reps {
			reps = max(reps, value)
		}
		estimate := latest.WeightKg * (1 + float64(reps)/30)
		return fmt.Sprintf("SANDBOX: оценка 1ПМ для «%s» — %.1f кг. Изображение и Telegram-сообщение не отправлялись.", exercise.Name, estimate), nil
	default:
		return "", fmt.Errorf("неизвестный sandbox-инструмент: %s", name)
	}
}

func (s *ToolService) findExercise(ctx context.Context, name string) (workout.Exercise, error) {
	exercises, err := s.workout.ListExercises(ctx)
	if err != nil {
		return workout.Exercise{}, err
	}
	needle := strings.ToLower(strings.TrimSpace(name))
	matches := []workout.Exercise{}
	for _, exercise := range exercises {
		candidate := strings.ToLower(exercise.Name)
		if candidate == needle || strings.Contains(candidate, needle) || strings.Contains(needle, candidate) {
			matches = append(matches, exercise)
		}
	}
	if len(matches) == 0 {
		return workout.Exercise{}, fmt.Errorf("упражнение %q не найдено", name)
	}
	if len(matches) > 1 {
		return workout.Exercise{}, fmt.Errorf("упражнение %q неоднозначно", name)
	}
	return matches[0], nil
}

func (s *ToolService) today() string {
	return s.todayTime().Format(time.DateOnly)
}

func (s *ToolService) todayTime() time.Time {
	return s.now().In(loadLocation(valueOr(s.zoneID, "Europe/Moscow")))
}

func isSandboxID(id int64) bool { return id >= sandboxIDMinimum && id <= sandboxIDMaximum }

func requiredString(args map[string]any, key string) string {
	value := optionalString(args, key)
	if value == "" {
		panicArgument("Нужно поле " + key)
	}
	return value
}

func optionalString(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	value, ok := args[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return typed.String()
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func requiredFloat(args map[string]any, key string) (float64, error) {
	raw := strings.ReplaceAll(optionalString(args, key), ",", ".")
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, fmt.Errorf("неверное число в %s", key)
	}
	return value, nil
}

func optionalInt(args map[string]any, key string, fallback, minimum, maximum int) int {
	value, err := strconv.Atoi(optionalString(args, key))
	if err != nil {
		return fallback
	}
	return min(max(value, minimum), maximum)
}

func resolveDates(args map[string]any, today string) ([]string, error) {
	if raw := optionalString(args, "days"); raw != "" {
		parts := strings.Split(raw, ",")
		result := []string{}
		for _, part := range parts {
			date := strings.TrimSpace(part)
			if _, err := time.Parse(time.DateOnly, date); err != nil {
				return nil, fmt.Errorf("неверная дата %q", date)
			}
			result = append(result, date)
		}
		if len(result) > 31 {
			return nil, errors.New("слишком много дней")
		}
		return result, nil
	}
	from, to := optionalString(args, "from"), optionalString(args, "to")
	if from == "" && to == "" {
		return []string{today}, nil
	}
	fromDate, fromErr := time.Parse(time.DateOnly, from)
	toDate, toErr := time.Parse(time.DateOnly, to)
	if fromErr != nil || toErr != nil || fromDate.After(toDate) {
		return nil, errors.New("для интервала нужны корректные from и to")
	}
	result := []string{}
	for date := fromDate; !date.After(toDate); date = date.AddDate(0, 0, 1) {
		result = append(result, date.Format(time.DateOnly))
		if len(result) > 31 {
			return nil, errors.New("интервал слишком большой")
		}
	}
	return result, nil
}

func formatGridDays(grid workout.Grid, dates []string) string {
	blocks := []string{}
	for _, date := range dates {
		lines := []string{"Тренировка за " + date + ":"}
		for _, row := range grid.Rows {
			if cell, ok := row.Cells[date]; ok {
				lines = append(lines, fmt.Sprintf("• %s: %s", row.ExerciseName, cell.Display))
			}
		}
		if len(lines) == 1 {
			blocks = append(blocks, "За "+date+" записей нет.")
		} else {
			blocks = append(blocks, strings.Join(lines, "\n"))
		}
	}
	return strings.Join(blocks, "\n\n")
}

func sandboxExerciseByName(state *sandboxState, name string) (*sandboxExercise, error) {
	needle := strings.ToLower(strings.TrimSpace(name))
	var found *sandboxExercise
	for index := range state.Exercises {
		exercise := &state.Exercises[index]
		candidate := strings.ToLower(exercise.Name)
		if candidate == needle || strings.Contains(candidate, needle) || strings.Contains(needle, candidate) {
			if found != nil {
				return nil, fmt.Errorf("неоднозначное sandbox-упражнение %q", name)
			}
			found = exercise
		}
	}
	if found == nil {
		return nil, fmt.Errorf("в SANDBOX нет упражнения %q", name)
	}
	return found, nil
}

func sandboxForgetFact(state *sandboxState, id string) (string, error) {
	filtered := state.Facts[:0]
	removed := false
	for _, fact := range state.Facts {
		if fact.ID == id {
			removed = true
			continue
		}
		filtered = append(filtered, fact)
	}
	if !removed {
		return "", errors.New("sandbox-факт не найден")
	}
	state.Facts = filtered
	return "SANDBOX: факт удалён.", nil
}

func randomID(prefix string) string {
	bytes := make([]byte, 8)
	_, _ = rand.Read(bytes)
	return prefix + "-" + hex.EncodeToString(bytes)
}

func panicArgument(message string) { panic(argumentPanic(message)) }

type argumentPanic string

func (a argumentPanic) Error() string { return string(a) }
