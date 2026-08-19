package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/alexey-va/my-utils-api/internal/health"
	"github.com/alexey-va/my-utils-api/internal/openrouter"
	"github.com/alexey-va/my-utils-api/internal/workout"
	"github.com/jackc/pgx/v5"
)

type ContextualConversation struct {
	memory           *Memory
	workout          *workout.Service
	health           *health.Service
	calendarDays     func() int
	recentEntries    func() int
	progressSessions func() int
	zoneID           func() string
}

func NewContextualConversation(
	memory *Memory,
	workoutService *workout.Service,
	healthService *health.Service,
	calendarDays func() int,
	recentEntries func() int,
	progressSessions func() int,
	zoneID func() string,
) *ContextualConversation {
	return &ContextualConversation{
		memory: memory, workout: workoutService, health: healthService,
		calendarDays: calendarDays, recentEntries: recentEntries,
		progressSessions: progressSessions, zoneID: zoneID,
	}
}

func (c *ContextualConversation) Append(ctx context.Context, chatID int64, message openrouter.Message) (Message, error) {
	return c.memory.Append(ctx, chatID, message)
}

func (c *ContextualConversation) Context(ctx context.Context, chatID int64, limit int) ([]openrouter.Message, error) {
	return c.memory.Context(ctx, chatID, limit)
}

func (c *ContextualConversation) PromptContext(ctx context.Context, chatID int64) (string, error) {
	memory, err := c.memory.PromptContext(ctx, chatID)
	if err != nil {
		return "", err
	}
	var snapshot string
	if isSandboxID(chatID) {
		snapshot, err = c.sandboxSnapshot(ctx, chatID)
	} else {
		snapshot, err = c.realSnapshot(ctx)
	}
	if err != nil {
		return "", err
	}
	if memory == "" {
		return snapshot, nil
	}
	return snapshot + "\n\n" + memory, nil
}

func (c *ContextualConversation) sandboxSnapshot(ctx context.Context, chatID int64) (string, error) {
	var raw string
	if err := c.memory.pool.QueryRow(ctx, `SELECT state_json FROM agent_test_sandbox_states WHERE memory_chat_id=$1`, chatID).Scan(&raw); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", errors.New("sandbox state not found; refusing real-data fallback")
		}
		return "", err
	}
	state := sandboxState{}
	if strings.TrimSpace(raw) != "" && strings.TrimSpace(raw) != "{}" {
		if err := json.Unmarshal([]byte(raw), &state); err != nil {
			return "", fmt.Errorf("decode sandbox state: %w", err)
		}
	}
	return FormatSandboxSnapshot(state), nil
}

func (c *ContextualConversation) realSnapshot(ctx context.Context) (string, error) {
	grid, err := c.workout.Grid(ctx)
	if err != nil {
		return "", err
	}
	exercises, err := c.workout.ListExercises(ctx)
	if err != nil {
		return "", err
	}
	now := time.Now().In(loadLocation(valueOr(c.zoneID, "Europe/Moscow")))
	weights, err := c.health.WeightHistory(ctx, 14, now)
	if err != nil {
		return "", err
	}
	return FormatFreshSnapshot(now, grid, exercises, weights, snapshotLimits{
		calendarDays:     intOr(c.calendarDays, 14, 1, 31),
		recentEntries:    intOr(c.recentEntries, 30, 1, 100),
		progressSessions: intOr(c.progressSessions, 4, 1, 12),
	}), nil
}

type snapshotLimits struct {
	calendarDays, recentEntries, progressSessions int
}

type snapshotEntry struct {
	date, exerciseID, exerciseName, muscleGroup, display string
}

func FormatFreshSnapshot(now time.Time, grid workout.Grid, exercises []workout.Exercise, weights health.WeightHistory, limits snapshotLimits) string {
	today := now.Format(time.DateOnly)
	weekStart := startOfWeek(now)
	weekEnd := weekStart.AddDate(0, 0, 6)
	groupByID := make(map[string]string, len(exercises))
	for _, exercise := range exercises {
		groupByID[exercise.ID] = exercise.MuscleGroup
	}
	entries := make([]snapshotEntry, 0)
	for _, row := range grid.Rows {
		for date, cell := range row.Cells {
			entries = append(entries, snapshotEntry{date: date, exerciseID: row.ExerciseID, exerciseName: row.ExerciseName, muscleGroup: groupByID[row.ExerciseID], display: cell.Display})
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].date == entries[j].date {
			return entries[i].exerciseName < entries[j].exerciseName
		}
		return entries[i].date > entries[j].date
	})

	doneThisWeek := map[string]bool{}
	lastByExercise := map[string]snapshotEntry{}
	for _, entry := range entries {
		if _, ok := lastByExercise[entry.exerciseID]; !ok {
			lastByExercise[entry.exerciseID] = entry
		}
		if entry.date >= weekStart.Format(time.DateOnly) && entry.date <= today {
			doneThisWeek[entry.exerciseID] = true
		}
	}

	var builder strings.Builder
	builder.WriteString("## Актуальный снимок дневника\n")
	builder.WriteString("Данные ниже уже в контексте — для плана, статистики и прогресса НЕ вызывай get_days / get_progress / list_exercises, если пользователь не просит дату вне календаря.\n")
	fmt.Fprintf(&builder, "Сейчас: %s (%s)\n", now.Format("02.01.2006 15:04"), now.Location())
	fmt.Fprintf(&builder, "Сегодня: %s (%s); завтра: %s (%s); неделя: %s–%s (понедельник–воскресенье)\n\n",
		today, weekdayRU(now.Weekday()), now.AddDate(0, 0, 1).Format(time.DateOnly), weekdayRU(now.AddDate(0, 0, 1).Weekday()), weekStart.Format("02.01"), weekEnd.Format("02.01"))

	builder.WriteString("### Вес тела\n")
	latestWeight, latestDate := weights.LatestWeightKg, weights.LatestDate
	if (latestWeight == nil || latestDate == nil) && len(weights.Days) > 0 {
		last := weights.Days[len(weights.Days)-1]
		latestWeight, latestDate = &last.WeightKg, &last.Date
	}
	if latestWeight == nil || latestDate == nil {
		builder.WriteString("Замеров веса пока нет.\n\n")
	} else {
		fmt.Fprintf(&builder, "Последний замер: %.1f кг (%s).\n", *latestWeight, *latestDate)
		for index := len(weights.Days) - 1; index >= 0; index-- {
			fmt.Fprintf(&builder, "• %s: %.1f кг\n", weights.Days[index].Date, weights.Days[index].WeightKg)
		}
		builder.WriteByte('\n')
	}

	builder.WriteString("### Список упражнений (" + fmt.Sprint(len(exercises)) + ")\n")
	if len(exercises) == 0 {
		builder.WriteString("— упражнений нет\n")
	} else {
		sortedExercises := append([]workout.Exercise(nil), exercises...)
		sort.Slice(sortedExercises, func(i, j int) bool {
			if sortedExercises[i].MuscleGroup == sortedExercises[j].MuscleGroup {
				return sortedExercises[i].Name < sortedExercises[j].Name
			}
			return sortedExercises[i].MuscleGroup < sortedExercises[j].MuscleGroup
		})
		for _, exercise := range sortedExercises {
			fmt.Fprintf(&builder, "• «%s» (%s)\n", exercise.Name, muscleGroupRU(exercise.MuscleGroup))
		}
	}

	fmt.Fprintf(&builder, "\n### Календарь %d дней (новые сверху)\n", limits.calendarDays)
	for offset := 0; offset < limits.calendarDays; offset++ {
		date := now.AddDate(0, 0, -offset).Format(time.DateOnly)
		fmt.Fprintf(&builder, "• %s: %s\n", displayDate(date), joinEntries(entriesOn(entries, date)))
	}

	fmt.Fprintf(&builder, "\n### Последние %d записей дневника\n", limits.recentEntries)
	if len(entries) == 0 {
		builder.WriteString("Записей пока нет.\n")
	} else {
		for _, entry := range entries[:min(len(entries), limits.recentEntries)] {
			fmt.Fprintf(&builder, "• %s «%s» (%s): %s\n", displayDate(entry.date), entry.exerciseName, muscleGroupRU(entry.muscleGroup), entry.display)
		}
	}

	fmt.Fprintf(&builder, "\n### История по упражнениям (до %d сессий, новые слева)\n", limits.progressSessions)
	withHistory := 0
	for _, exercise := range exercises {
		history := entriesFor(entries, exercise.ID, limits.progressSessions)
		if len(history) == 0 {
			continue
		}
		withHistory++
		parts := make([]string, len(history))
		for index, entry := range history {
			parts[index] = displayDate(entry.date) + " " + entry.display
		}
		fmt.Fprintf(&builder, "• «%s»: %s\n", exercise.Name, strings.Join(parts, " ← "))
	}
	if withHistory == 0 {
		builder.WriteString("Записей пока нет.\n")
	}

	builder.WriteString("\n### Эта неделя — уже сделано\n")
	weekly := entriesBetween(entries, weekStart.Format(time.DateOnly), today)
	if len(weekly) == 0 {
		builder.WriteString("На этой неделе записей ещё нет.\n")
	} else {
		for index := len(weekly) - 1; index >= 0; index-- {
			entry := weekly[index]
			fmt.Fprintf(&builder, "• %s «%s» (%s): %s\n", displayDate(entry.date), entry.exerciseName, muscleGroupRU(entry.muscleGroup), entry.display)
		}
	}

	builder.WriteString("\n### Эта неделя — ещё не делали (из списка упражнений)\n")
	pending := 0
	for _, exercise := range exercises {
		if doneThisWeek[exercise.ID] {
			continue
		}
		pending++
		last, ok := lastByExercise[exercise.ID]
		lastLine := "в дневнике ещё не записывали"
		if ok {
			lastLine = "последний раз " + displayDate(last.date) + ": " + last.display
		}
		fmt.Fprintf(&builder, "• «%s» (%s) — %s\n", exercise.Name, muscleGroupRU(exercise.MuscleGroup), lastLine)
	}
	if pending == 0 {
		builder.WriteString("Все упражнения из списка уже были на этой неделе.\n")
	}

	builder.WriteString("\n### Баланс групп мышц на неделе\n")
	appendGroupBalance(&builder, exercises, doneThisWeek)
	builder.WriteString("\nПодсказка для плана: чередуй группы (грудь+трицепс, спина+бицепс, ноги, плечи). Приоритет — упражнения из «ещё не делали» и группы с 0 сессий на неделе.\n")

	for _, section := range []struct{ title, date string }{{"Сегодня", today}, {"Вчера", now.AddDate(0, 0, -1).Format(time.DateOnly)}} {
		fmt.Fprintf(&builder, "\n### %s\n", section.title)
		items := entriesOn(entries, section.date)
		if len(items) == 0 {
			fmt.Fprintf(&builder, "За %s записей нет.\n", displayDate(section.date))
		} else {
			for _, entry := range items {
				fmt.Fprintf(&builder, "• %s: %s\n", entry.exerciseName, entry.display)
			}
		}
	}

	builder.WriteString("\n### Все упражнения (последняя сессия — для расчёта весов)\n")
	if len(exercises) == 0 {
		builder.WriteString("— упражнений нет\n")
	} else {
		for _, exercise := range exercises {
			last, ok := lastByExercise[exercise.ID]
			line := "нет записей"
			if ok {
				line = displayDate(last.date) + " — " + last.display
			}
			fmt.Fprintf(&builder, "• «%s» (%s): %s\n", exercise.Name, muscleGroupRU(exercise.MuscleGroup), line)
		}
	}
	return strings.TrimSpace(builder.String())
}

func FormatSandboxSnapshot(state sandboxState) string {
	exercises := append([]sandboxExercise(nil), state.Exercises...)
	sort.Slice(exercises, func(i, j int) bool { return strings.ToLower(exercises[i].Name) < strings.ToLower(exercises[j].Name) })
	workouts := append([]sandboxWorkout(nil), state.Workouts...)
	sort.Slice(workouts, func(i, j int) bool {
		if workouts[i].PerformedOn == workouts[j].PerformedOn {
			return workouts[i].ExerciseName < workouts[j].ExerciseName
		}
		return workouts[i].PerformedOn > workouts[j].PerformedOn
	})
	weights := append([]sandboxWeight(nil), state.BodyWeights...)
	sort.Slice(weights, func(i, j int) bool { return weights[i].PerformedOn > weights[j].PerformedOn })
	var builder strings.Builder
	builder.WriteString("## ИЗОЛИРОВАННЫЙ SANDBOX\n")
	builder.WriteString("Это отдельный тестовый контекст. В нём нет реальных Workout-данных, Telegram-доставки или production-уведомлений. Все изменения tools остаются только внутри этого тестового чата.\n\n")
	builder.WriteString("### Упражнения\n")
	if len(exercises) == 0 {
		builder.WriteString("— упражнений нет\n")
	} else {
		for _, exercise := range exercises {
			fmt.Fprintf(&builder, "• «%s» (%s) [sandbox_id=%s]\n", exercise.Name, exercise.MuscleGroup, exercise.ID)
		}
	}
	builder.WriteString("\n### Тренировки\n")
	if len(workouts) == 0 {
		builder.WriteString("— записей нет\n")
	} else {
		for _, row := range workouts[:min(20, len(workouts))] {
			fmt.Fprintf(&builder, "• %s: %s — %s\n", row.PerformedOn, row.ExerciseName, workout.Display(row.WeightKg, row.Reps, row.Weights))
		}
	}
	builder.WriteString("\n### Вес тела\n")
	if len(weights) == 0 {
		builder.WriteString("— замеров нет\n")
	} else {
		for _, row := range weights[:min(10, len(weights))] {
			fmt.Fprintf(&builder, "• %s: %.1f кг\n", row.PerformedOn, row.WeightKg)
		}
	}
	builder.WriteString("\n### Изолированные sandbox-факты\n")
	if len(state.Facts) == 0 {
		builder.WriteString("— фактов нет\n")
	} else {
		for _, fact := range state.Facts {
			fmt.Fprintf(&builder, "• [%s] %s\n", fact.ID, fact.Content)
		}
	}
	return strings.TrimSpace(builder.String())
}

func entriesOn(entries []snapshotEntry, date string) []snapshotEntry {
	result := []snapshotEntry{}
	for _, entry := range entries {
		if entry.date == date {
			result = append(result, entry)
		}
	}
	return result
}

func entriesBetween(entries []snapshotEntry, from, to string) []snapshotEntry {
	result := []snapshotEntry{}
	for _, entry := range entries {
		if entry.date >= from && entry.date <= to {
			result = append(result, entry)
		}
	}
	return result
}

func entriesFor(entries []snapshotEntry, exerciseID string, limit int) []snapshotEntry {
	result := []snapshotEntry{}
	for _, entry := range entries {
		if entry.exerciseID == exerciseID {
			result = append(result, entry)
			if len(result) == limit {
				break
			}
		}
	}
	return result
}

func joinEntries(entries []snapshotEntry) string {
	if len(entries) == 0 {
		return "—"
	}
	items := make([]string, len(entries))
	for index, entry := range entries {
		items[index] = "«" + entry.exerciseName + "» " + entry.display
	}
	return strings.Join(items, "; ")
}

func appendGroupBalance(builder *strings.Builder, exercises []workout.Exercise, done map[string]bool) {
	groups := map[string][]workout.Exercise{}
	for _, exercise := range exercises {
		groups[exercise.MuscleGroup] = append(groups[exercise.MuscleGroup], exercise)
	}
	keys := make([]string, 0, len(groups))
	for group := range groups {
		keys = append(keys, group)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		builder.WriteString("— упражнений нет\n")
		return
	}
	for _, group := range keys {
		doneNames, pendingNames := []string{}, []string{}
		for _, exercise := range groups[group] {
			name := "«" + exercise.Name + "»"
			if done[exercise.ID] {
				doneNames = append(doneNames, name)
			} else {
				pendingNames = append(pendingNames, name)
			}
		}
		fmt.Fprintf(builder, "• %s: %d/%d на неделе | сделано: %s | осталось: %s\n", muscleGroupRU(group), len(doneNames), len(groups[group]), dashIfEmpty(doneNames), dashIfEmpty(pendingNames))
	}
}

func startOfWeek(value time.Time) time.Time {
	daysSinceMonday := (int(value.Weekday()) + 6) % 7
	return time.Date(value.Year(), value.Month(), value.Day()-daysSinceMonday, 0, 0, 0, 0, value.Location())
}

func displayDate(value string) string {
	parsed, err := time.Parse(time.DateOnly, value)
	if err != nil {
		return value
	}
	return parsed.Format("02.01")
}

func weekdayRU(day time.Weekday) string {
	return []string{"воскресенье", "понедельник", "вторник", "среда", "четверг", "пятница", "суббота"}[day]
}

func muscleGroupRU(group string) string {
	if value, ok := map[string]string{"chest": "грудь", "back": "спина", "legs": "ноги", "shoulders": "плечи", "arms": "руки", "core": "кор", "other": "другое"}[strings.ToLower(group)]; ok {
		return value
	}
	return group
}

func dashIfEmpty(values []string) string {
	if len(values) == 0 {
		return "—"
	}
	return strings.Join(values, ", ")
}

func loadLocation(zone string) *time.Location {
	location, err := time.LoadLocation(strings.TrimSpace(zone))
	if err != nil {
		return time.UTC
	}
	return location
}

func valueOr(value func() string, fallback string) string {
	if value == nil || strings.TrimSpace(value()) == "" {
		return fallback
	}
	return strings.TrimSpace(value())
}

func intOr(value func() int, fallback, minimum, maximum int) int {
	if value == nil {
		return fallback
	}
	return min(max(value(), minimum), maximum)
}
