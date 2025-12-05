package fields

import (
	"strings"
	"unicode"
)

const (
	lineLength = 77
)

func wrapLine(line string) []string {
	if len(line) <= lineLength {
		return []string{line}
	}
	tokens := strings.Split(line, " ")
	lines := []string{}
	currentLine := ""
	for _, token := range tokens {
		appendedLength := len(token)
		if currentLine != "" {
			appendedLength++
		}
		newLength := appendedLength + len(currentLine)
		if newLength > lineLength {
			lines = append(lines, currentLine)
			currentLine = ""
		}
		if currentLine == "" {
			currentLine = token
			continue
		}
		currentLine = currentLine + " " + token
	}
	return append(lines, currentLine)
}

func goName(s string) string {
	if s == "" {
		return ""
	}

	norm := strings.Map(func(r rune) rune {
		switch {
		case r == '_' || r == '-' || r == '.' || unicode.IsSpace(r):
			return '_'
		default:
			return r
		}
	}, s)

	parts := strings.Split(norm, "_")
	var b strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}

		cleaned := strings.Map(func(r rune) rune {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				return r
			}
			return -1
		}, p)
		if cleaned == "" {
			continue
		}

		runes := []rune(cleaned)
		runes[0] = unicode.ToUpper(runes[0])
		b.WriteString(string(runes))
	}

	out := b.String()
	if out == "" {
		return ""
	}

	return out
}
