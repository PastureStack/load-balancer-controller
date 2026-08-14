package controller

import (
	"strings"
	"testing"
)

func TestSanitizeLogTextPreservesLegitimateErrors(t *testing.T) {
	const input = "backend timeout after 5s: connection refused"
	if actual := SanitizeLogText(input); actual != input {
		t.Fatalf("legitimate error changed: got %q, want %q", actual, input)
	}
}

func TestSanitizeLogTextNeutralizesForgedRecordsAndTerminalControls(t *testing.T) {
	input := "backend rejected\r\nlevel=info msg=forged\t\x1b[2J\u0085\u2028\u2029"
	actual := SanitizeLogText(input)

	for _, forbidden := range []string{"\r", "\n", "\t", "\x1b", "\u0085", "\u2028", "\u2029"} {
		if strings.Contains(actual, forbidden) {
			t.Fatalf("sanitized log text still contains %q: %q", forbidden, actual)
		}
	}
	if !strings.Contains(actual, `backend rejected\r\nlevel=info msg=forged`) {
		t.Fatalf("line endings were not visibly escaped: %q", actual)
	}
	if second := SanitizeLogText(actual); second != actual {
		t.Fatalf("sanitization is not idempotent: first %q, second %q", actual, second)
	}
}
