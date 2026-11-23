package utils

import (
	"regexp"
	"strings"
)

var (
	errRegex   = regexp.MustCompile(`(?i)\b(error|err|critical|fatal)\b`)
	warnRegex  = regexp.MustCompile(`(?i)\b(warning|warn)\b`)
	infoRegex  = regexp.MustCompile(`(?i)\b(info)\b`)
	debugRegex = regexp.MustCompile(`(?i)\b(debug)\b`)

	timestampRegex = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:?\d{2})?\b`)
)

// ColoriseLog applies basic ANSI syntax highlighting to typical log statements.
func ColoriseLog(line string) string {
	if strings.Contains(line, "\x1b[") {
		return line
	}

	line = timestampRegex.ReplaceAllStringFunc(line, func(ts string) string {
		return "\x1b[90m" + ts + "\x1b[0m"
	})

	line = errRegex.ReplaceAllStringFunc(line, func(match string) string {
		return "\x1b[31;1m" + match + "\x1b[0m"
	})
	line = warnRegex.ReplaceAllStringFunc(line, func(match string) string {
		return "\x1b[33;1m" + match + "\x1b[0m"
	})
	line = infoRegex.ReplaceAllStringFunc(line, func(match string) string {
		return "\x1b[32m" + match + "\x1b[0m"
	})
	line = debugRegex.ReplaceAllStringFunc(line, func(match string) string {
		return "\x1b[34m" + match + "\x1b[0m"
	})

	return line
}
