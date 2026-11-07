package agent

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const safeReplyFallback = "Не удалось сформировать корректный ответ. Повтори, пожалуйста."

func workoutReceipt(name, date, sourceDate string, weight float64, reps, weights []int) string {
	details := receiptKg(weight)
	if len(reps) != 0 {
		switch {
		case len(weights) == len(reps):
			sets := make([]string, len(reps))
			for i, count := range reps {
				sets[i] = fmt.Sprintf("%d кг × %d", weights[i], count)
			}
			details = strings.Join(sets, ", ")
		case slices.Min(reps) == slices.Max(reps):
			details += fmt.Sprintf(", %d×%d", len(reps), reps[0])
		default:
			sets := make([]string, len(reps))
			for i, count := range reps {
				sets[i] = strconv.Itoa(count)
			}
			details += ", повторы по подходам: " + strings.Join(sets, " / ")
		}
	}
	if sourceDate != "" && sourceDate != date {
		details += " (из записи за " + receiptDate(sourceDate) + ")"
	}
	return fmt.Sprintf("Записал за %s: %s — %s.", receiptDate(date), name, details)
}

func bodyWeightReceipt(date string, weight float64, created bool) string {
	verb := "Обновил"
	if created {
		verb = "Записал"
	}
	return fmt.Sprintf("%s вес за %s: %s.", verb, receiptDate(date), receiptKg(weight))
}

func receiptKg(weight float64) string {
	return strings.Replace(strconv.FormatFloat(weight, 'f', -1, 64), ".", ",", 1) + " кг"
}

func receiptDate(date string) string {
	if parsed, err := time.Parse(time.DateOnly, date); err == nil {
		months := [...]string{"января", "февраля", "марта", "апреля", "мая", "июня", "июля", "августа", "сентября", "октября", "ноября", "декабря"}
		return fmt.Sprintf("%d %s %d года", parsed.Day(), months[parsed.Month()-1], parsed.Year())
	}
	return date
}

// Keep references in the tool receipt so an explicit technical request or a
// follow-up cancellation can still find them in the archive.
func visibleMutationReceipt(action plannedAction, result string) string {
	switch action.Tool {
	case "remember_fact":
		return "Запомнил."
	case "forget_fact":
		return "Убрал это из памяти."
	case "manage_user_fact":
		switch action.Arguments["action"] {
		case "remember":
			return "Запомнил."
		case "forget":
			return "Убрал это из памяти."
		case "update":
			return "Обновил заметку."
		}
	case "send_notification", "schedule_notification", "send_weekly_health_report", "cancel_notification":
		// The notification adapters append a service-owned workflow suffix.
		if text, ref, ok := strings.Cut(result, " (workflow "); ok && strings.HasSuffix(ref, ").") {
			return text + "."
		}
	}
	return result
}

func visibleMutationError(action plannedAction, err error) string {
	var exerciseErr *ExerciseResolutionError
	if errors.As(err, &exerciseErr) {
		switch exerciseErr.Kind {
		case ExerciseResolutionAmbiguous:
			names := make([]string, len(exerciseErr.Candidates))
			for i, candidate := range exerciseErr.Candidates {
				names[i] = fmt.Sprintf("«%s» (%s)", candidate.Name, muscleGroupRU(candidate.MuscleGroup))
			}
			return "Уточни, какое упражнение ты имеешь в виду: " + strings.Join(names, ", ") + "?"
		default:
			return "Не нашёл это упражнение в дневнике. Как оно называется?"
		}
	}
	prefix := "Не удалось подтвердить действие"
	for _, key := range []string{"exercise_name", "name", "current_name"} {
		if name, ok := action.Arguments[key].(string); ok {
			prefix += " для «" + name + "»"
			break
		}
	}
	return prefix + ": " + err.Error()
}

var (
	fencedCodeMarker            = regexp.MustCompile(`(?m)^[ \t]*` + "```" + `(?:\w+)?[ \t]*$`)
	markdownHeading             = regexp.MustCompile(`(?m)^[ \t]{0,3}#{1,6}[ \t]+(.+?)[ \t]*$`)
	markdownBold                = regexp.MustCompile(`\*\*([^*\n]+)\*\*|__([^_\n]+)__`)
	markdownCode                = regexp.MustCompile("`([^`\\n]+)`")
	markdownBullet              = regexp.MustCompile(`(?m)^[ \t]*[-*+][ \t]+`)
	markdownLink                = regexp.MustCompile(`\[([^\]\n]+)]\((https?://[^)\s]+)\)`)
	internalHistoryTimestamp    = regexp.MustCompile(`^(?:\[Отправлено \d{2}\.\d{2}\.\d{4} \d{2}:\d{2} [A-Za-z0-9_+./-]+\][ \t]*)+`)
	encodedHorizontalWhitespace = regexp.MustCompile(`(?i)(?:&#x0*(?:20|a0);|&#0*(?:32|160);|&nbsp;)`)
	horizontalWhitespace        = regexp.MustCompile(`[ \t]{2,}`)
	lineLeadingWhitespace       = regexp.MustCompile(`(?m)^[ \t]+`)
	whitespaceBeforePunctuation = regexp.MustCompile(`[ \t]+([,.!?;:])`)
	excessiveBlankLines         = regexp.MustCompile(`\n{3,}`)
)

// NormalizeReply converts the model's common Markdown subset to Telegram HTML.
func NormalizeReply(raw string) string {
	text := stripInternalHistoryPrefix(raw)
	if LooksInvalidForRussianUser(text) {
		return safeReplyFallback
	}
	text = fencedCodeMarker.ReplaceAllString(text, "")
	text = markdownHeading.ReplaceAllString(text, "<b>$1</b>")
	text = markdownBold.ReplaceAllStringFunc(text, func(match string) string {
		parts := markdownBold.FindStringSubmatch(match)
		content := parts[1]
		if content == "" {
			content = parts[2]
		}
		return "<b>" + content + "</b>"
	})
	text = markdownCode.ReplaceAllString(text, "<code>$1</code>")
	text = markdownBullet.ReplaceAllString(text, "• ")
	text = markdownLink.ReplaceAllString(text, "$1 ($2)")
	text = strings.ReplaceAll(text, "`", "")
	text = encodedHorizontalWhitespace.ReplaceAllString(text, " ")
	text = horizontalWhitespace.ReplaceAllString(text, " ")
	text = lineLeadingWhitespace.ReplaceAllString(text, "")
	text = whitespaceBeforePunctuation.ReplaceAllString(text, "$1")
	text = excessiveBlankLines.ReplaceAllString(text, "\n\n")
	text = strings.TrimSpace(text)
	if text == "" {
		return "Готово."
	}
	return text
}

func stripInternalHistoryPrefix(text string) string {
	return strings.TrimSpace(internalHistoryTimestamp.ReplaceAllString(strings.TrimSpace(text), ""))
}

func LooksInvalidForRussianUser(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	if strings.HasPrefix(text, "[Отправ") || strings.Contains(strings.ToLower(text), "output truncated") {
		return true
	}
	for _, marker := range []string{"作为一个人工智能语言模型", "我还没学习如何回答这个问题"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	han, cyrillic := 0, 0
	for _, value := range text {
		switch {
		case unicode.Is(unicode.Han, value):
			han++
		case unicode.Is(unicode.Cyrillic, value):
			cyrillic++
		}
	}
	return han >= 4 && han > cyrillic
}
