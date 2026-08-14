package controller

import (
	"strings"
	"unicode"
)

// SanitizeLogText keeps untrusted values in one physical log record and
// neutralizes terminal control characters without changing ordinary errors.
func SanitizeLogText(input string) string {
	input = strings.Map(func(r rune) rune {
		switch r {
		case '\r', '\n':
			return r
		case '\u0085', '\u2028', '\u2029':
			return '\uFFFD'
		default:
			if unicode.IsControl(r) {
				return '\uFFFD'
			}
			return r
		}
	}, input)

	input = strings.ReplaceAll(input, "\r", `\r`)
	return strings.ReplaceAll(input, "\n", `\n`)
}
