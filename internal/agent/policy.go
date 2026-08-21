package agent

import (
	"regexp"
	"strings"
)

var (
	workoutNotation = regexp.MustCompile(`(?i)(?:^|[^\d.,/])\d+(?:[.,]\d+)?\s*(?:кг|kg)?\s+(?:\d+\s*[*xх×]\s*)?\d+(?:\s*/\s*\d+)+(?:$|[^\d/])`)
	workoutWrite    = regexp.MustCompile(`(?i)(?:^|\s)(?:запиши(?:те)?|записать|залогируй(?:те)?|зафиксируй(?:те)?|добавь(?:те)?\s+(?:тренировку|в\s+дневник))(?:\s|$)`)
	deleteWrite     = regexp.MustCompile(`(?i)(?:^|\s)(?:удали(?:те)?|удалить|сотри(?:те)?|стереть|убери(?:те)?|исправь(?:те)?(?:\s+запись)?)(?:\s|$)`)
	exerciseCreate  = regexp.MustCompile(`(?i)(?:^|\s)(?:создай(?:те)?|создать|добавь(?:те)?)(?:\s+упражнение)?(?:\s|$)`)
	exerciseRename  = regexp.MustCompile(`(?i)(?:переименуй(?:те)?|переименовать|смени(?:те)?\s+название)`)
	bodyWeight      = regexp.MustCompile(`(?i)\d+(?:[.,]\d+)?(?:\s*(?:кг|kg|lb|lbs|фунт(?:а|ов)?))?`)
	bareWeight      = regexp.MustCompile(`(?i)^(?:вес\s*[:=]?\s*)?\d+(?:[.,]\d+)?\s*(?:кг|kg|lb|lbs|фунт(?:а|ов)?)[.!]?$`)
	weightStatement = regexp.MustCompile(`(?i)(?:взвесил(?:ся|ась)?|мой\s+вес|вешу).{0,20}\d`)
	naturalWeight   = regexp.MustCompile(`(?i)(?:^|\s)(?:вес(?:\s+(?:сегодня|вчера))?|(?:сегодня|вчера)\s+вес)\s*[:=—-]?\s*\d`)
	weightWrite     = regexp.MustCompile(`(?i)(?:запиши(?:те)?|записать|зафиксируй(?:те)?)\s+(?:мой\s+)?вес`)
	rememberWrite   = regexp.MustCompile(`(?i)(?:^|\s)(?:запомни(?:те)?|учти(?:те)?|сохрани(?:те)?\s+(?:как\s+)?факт)(?:\s|[:—-]|$)`)
	forgetWrite     = regexp.MustCompile(`(?i)(?:^|\s)(?:забудь(?:те)?|удали(?:те)?\s+факт|больше\s+не\s+учитывай(?:те)?)(?:\s|$)`)
	notifyWrite     = regexp.MustCompile(`(?i)(?:^|\s)(?:напомни(?:те)?|уведоми(?:те)?|поставь(?:те)?\s+напоминание|запланируй(?:те)?\s+(?:напоминание|уведомление))(?:\s|$)`)
	cancelNotify    = regexp.MustCompile(`(?i)(?:отмени(?:те)?|удали(?:те)?|сними(?:те)?)\s+(?:(?:это|последнее|предыдущее)\s+)?(?:напоминание|уведомление)`)
	readQuestion    = regexp.MustCompile(`(?i)^(?:что|ка(?:к|кой|кая|кие)|сколько|когда|почему|зачем|где|покажи|расскажи)(?:\s|$)`)
	kilogramUnit    = regexp.MustCompile(`(?i)\s*(?:кг|kg)\s*`)
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

func GroundToolArguments(toolName string, args map[string]any, userMessage string) {
	if NormalizeToolName(toolName) != "log_workout" {
		return
	}
	match := workoutNotation.FindString(userMessage)
	match = strings.TrimSpace(match)
	match = strings.Trim(match, ".,;:!?—–-()[]{}")
	match = kilogramUnit.ReplaceAllString(match, " ")
	match = strings.Join(strings.Fields(match), " ")
	if match != "" {
		args["notation"] = match
	}
}
