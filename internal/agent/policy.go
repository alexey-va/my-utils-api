package agent

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/alexey-va/my-utils-api/internal/openrouter"
	"github.com/alexey-va/my-utils-api/internal/workout"
)

var (
	workoutNotation  = regexp.MustCompile(`(?i)(?:^|[^\d.,/])(\d+(?:[.,]\d+)?)\s*(кг|kg|lb|lbs|фунт(?:а|ов)?)?\s+((?:\d+\s*[*xх×]\s*)?\d+(?:\s*/\s*\d+)+)(?:$|[^\d/])`)
	workoutWrite     = regexp.MustCompile(`(?i)(?:^|\s)(?:запиши(?:те)?|записать|залогируй(?:те)?|зафиксируй(?:те)?|добавь(?:те)?\s+(?:тренировку|в\s+дневник))(?:\s|$)`)
	deleteWrite      = regexp.MustCompile(`(?i)(?:^|\s)(?:удали(?:те)?|удалить|сотри(?:те)?|стереть|убери(?:те)?|исправь(?:те)?(?:\s+запись)?)(?:\s|$)`)
	exerciseCreate   = regexp.MustCompile(`(?i)(?:^|\s)(?:создай(?:те)?|создать|добавь(?:те)?)(?:\s+упражнение)?(?:\s|$)`)
	exerciseRename   = regexp.MustCompile(`(?i)(?:переименуй(?:те)?|переименовать|смени(?:те)?\s+название)`)
	bodyWeight       = regexp.MustCompile(`(?i)\d+(?:[.,]\d+)?(?:\s*(?:кг|kg|lb|lbs|фунт(?:а|ов)?))?`)
	bareWeight       = regexp.MustCompile(`(?i)^(?:вес\s*[:=]?\s*)?\d+(?:[.,]\d+)?\s*(?:кг|kg|lb|lbs|фунт(?:а|ов)?)[.!]?$`)
	weightStatement  = regexp.MustCompile(`(?i)(?:взвесил(?:ся|ась)?|мой\s+вес|вешу).{0,20}\d`)
	naturalWeight    = regexp.MustCompile(`(?i)(?:^|\s)(?:вес(?:\s+(?:сегодня|вчера))?|(?:сегодня|вчера)\s+вес)\s*[:=—-]?\s*\d`)
	weightWrite      = regexp.MustCompile(`(?i)(?:запиши(?:те)?|записать|зафиксируй(?:те)?)\s+(?:мой\s+)?вес`)
	rememberWrite    = regexp.MustCompile(`(?i)(?:^|\s)(?:запомни(?:те)?|учти(?:те)?|сохрани(?:те)?\s+(?:как\s+)?факт)(?:\s|[:—-]|$)`)
	forgetWrite      = regexp.MustCompile(`(?i)(?:^|\s)(?:забудь(?:те)?|удали(?:те)?\s+факт|больше\s+не\s+учитывай(?:те)?)(?:\s|$)`)
	notifyWrite      = regexp.MustCompile(`(?i)(?:^|\s)(?:напомни(?:те)?|уведоми(?:те)?|поставь(?:те)?\s+напоминание|запланируй(?:те)?\s+(?:напоминание|уведомление))(?:\s|$)`)
	cancelNotify     = regexp.MustCompile(`(?i)(?:отмени(?:те)?|удали(?:те)?|сними(?:те)?)\s+(?:(?:это|последнее|предыдущее)\s+)?(?:напоминание|уведомление)`)
	readQuestion     = regexp.MustCompile(`(?i)^(?:что|ка(?:к|кой|кая|кие)|сколько|когда|почему|зачем|где|покажи|расскажи)(?:\s|$)`)
	negativeFollowUp = regexp.MustCompile(`(?i)^(?:нет|не надо|не делай|не удаляй|отмена|отмени)[.!\s]*$`)
)

var mutatingTools = map[string]bool{
	"create_exercise": true, "rename_exercise": true, "log_workout": true, "delete_workout": true,
	"log_body_weight": true, "remember_fact": true, "forget_fact": true, "manage_user_fact": true,
	"send_notification": true, "schedule_notification": true, "cancel_notification": true,
}

func NormalizeToolName(value string) string {
	var output strings.Builder
	for index, current := range value {
		if index > 0 && current >= 'A' && current <= 'Z' {
			output.WriteByte('_')
		}
		output.WriteRune(current)
	}
	return strings.ToLower(output.String())
}

func MutationAllowed(toolName, userMessage string) bool {
	toolName = NormalizeToolName(toolName)
	if !mutatingTools[toolName] {
		return true
	}
	message := strings.ToLower(strings.TrimSpace(userMessage))
	question := strings.Contains(message, "?") || readQuestion.MatchString(message)
	correction := !question && deleteWrite.MatchString(message)
	workoutIntent := workoutWrite.MatchString(message) || correction || (!question && workoutNotation.MatchString(message))
	switch toolName {
	case "create_exercise":
		return !question && (exerciseCreate.MatchString(message) || workoutIntent)
	case "rename_exercise":
		return exerciseRename.MatchString(message)
	case "log_workout":
		return workoutIntent
	case "delete_workout":
		return deleteWrite.MatchString(message)
	case "log_body_weight":
		return bodyWeight.MatchString(message) && (weightWrite.MatchString(message) || (!question && (bareWeight.MatchString(message) || weightStatement.MatchString(message) || naturalWeight.MatchString(message))))
	case "remember_fact":
		return rememberWrite.MatchString(message)
	case "forget_fact":
		return forgetWrite.MatchString(message)
	case "manage_user_fact":
		return rememberWrite.MatchString(message) || forgetWrite.MatchString(message)
	case "send_notification", "schedule_notification":
		return notifyWrite.MatchString(message)
	case "cancel_notification":
		return cancelNotify.MatchString(message)
	default:
		return false
	}
}

func MutationAllowedWithContext(toolName, userMessage string, messages []openrouter.Message) bool {
	if MutationAllowed(toolName, userMessage) {
		return true
	}
	if !isMutationClarificationReply(userMessage) {
		return false
	}
	skippedCurrentUser := false
	foundClarification := false
	for index := len(messages) - 1; index >= 0; index-- {
		message := messages[index]
		switch strings.ToLower(strings.TrimSpace(message.Role)) {
		case "user":
			if !skippedCurrentUser {
				skippedCurrentUser = true
				continue
			}
			if !foundClarification {
				return false
			}
			return MutationAllowed(toolName, contentString(message.Content))
		case "assistant":
			if len(message.ToolCalls) > 0 {
				continue
			}
			if foundClarification {
				continue
			}
			if !looksLikeMutationClarification(contentString(message.Content)) {
				return false
			}
			foundClarification = true
		case "tool":
			continue
		}
	}
	return false
}

func isMutationClarificationReply(message string) bool {
	message = strings.TrimSpace(message)
	if message == "" || len([]rune(message)) > 160 || strings.Contains(message, "?") {
		return false
	}
	lower := strings.ToLower(message)
	return !readQuestion.MatchString(lower) && !negativeFollowUp.MatchString(lower)
}

func looksLikeMutationClarification(message string) bool {
	lower := strings.ToLower(strings.TrimSpace(message))
	for _, marker := range []string{"уточни", "подтверди", "подтвержда", "какую", "какой", "какое", "какая"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

type toolArgumentGrounder struct {
	workoutNotations []string
	used             []bool
}

func newToolArgumentGrounder(userMessage string) *toolArgumentGrounder {
	notations := extractWorkoutNotations(userMessage)
	return &toolArgumentGrounder{workoutNotations: notations, used: make([]bool, len(notations))}
}

func (g *toolArgumentGrounder) Ground(toolName string, args map[string]any) error {
	if NormalizeToolName(toolName) != "log_workout" || len(g.workoutNotations) == 0 {
		return nil
	}
	selected := -1
	modelNotation := optionalString(args, "notation")
	for index, notation := range g.workoutNotations {
		if !g.used[index] && workoutNotationsEquivalent(modelNotation, notation) {
			if selected >= 0 {
				selected = -1
				break
			}
			selected = index
		}
	}
	if selected < 0 {
		for index := range g.workoutNotations {
			if !g.used[index] {
				selected = index
				break
			}
		}
	}
	if selected < 0 {
		return fmt.Errorf("не удалось однозначно сопоставить данные упражнения с сообщением пользователя")
	}
	g.used[selected] = true
	args["notation"] = g.workoutNotations[selected]
	return nil
}

func GroundToolArguments(toolName string, args map[string]any, userMessage string) {
	_ = newToolArgumentGrounder(userMessage).Ground(toolName, args)
}

func extractWorkoutNotations(message string) []string {
	matches := workoutNotation.FindAllStringSubmatch(message, -1)
	result := make([]string, 0, len(matches))
	for _, match := range matches {
		weight, err := strconv.ParseFloat(strings.ReplaceAll(match[1], ",", "."), 64)
		if err != nil || weight <= 0 {
			continue
		}
		unit := strings.ToLower(strings.TrimSpace(match[2]))
		if unit == "lb" || unit == "lbs" || strings.HasPrefix(unit, "фунт") {
			weight = math.Round(weight * 0.45359237)
		}
		weightText := strconv.FormatFloat(weight, 'f', -1, 64)
		reps := strings.Join(strings.Fields(match[3]), "")
		result = append(result, weightText+" "+reps)
	}
	return result
}

func workoutNotationsEquivalent(first, second string) bool {
	left, leftErr := workout.ParseNotation(first)
	right, rightErr := workout.ParseNotation(second)
	if leftErr != nil || rightErr != nil {
		return false
	}
	return left.WeightKg == right.WeightKg && slices.Equal(left.Weights, right.Weights) && slices.Equal(left.Reps, right.Reps)
}
