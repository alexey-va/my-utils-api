package agent

import (
	"regexp"
	"strings"
	"unicode"
)

var (
	fencedCodeMarker            = regexp.MustCompile(`(?m)^[ \t]*` + "```" + `(?:\w+)?[ \t]*$`)
	markdownHeading             = regexp.MustCompile(`(?m)^[ \t]{0,3}#{1,6}[ \t]+(.+?)[ \t]*$`)
	markdownBold                = regexp.MustCompile(`\*\*([^*\n]+)\*\*|__([^_\n]+)__`)
	markdownCode                = regexp.MustCompile("`([^`\\n]+)`")
	markdownBullet              = regexp.MustCompile(`(?m)^[ \t]*[-*+][ \t]+`)
	markdownLink                = regexp.MustCompile(`\[([^\]\n]+)]\((https?://[^)\s]+)\)`)
	encodedHorizontalWhitespace = regexp.MustCompile(`(?i)(?:&#x0*(?:20|a0);|&#0*(?:32|160);|&nbsp;)`)
	horizontalWhitespace        = regexp.MustCompile(`[ \t]{2,}`)
	lineLeadingWhitespace       = regexp.MustCompile(`(?m)^[ \t]+`)
	whitespaceBeforePunctuation = regexp.MustCompile(`[ \t]+([,.!?;:])`)
	excessiveBlankLines         = regexp.MustCompile(`\n{3,}`)
)

// NormalizeReply converts the model's common Markdown subset to Telegram HTML.
func NormalizeReply(raw string) string {
	text := strings.TrimSpace(raw)
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

func LooksInvalidForRussianUser(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
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
