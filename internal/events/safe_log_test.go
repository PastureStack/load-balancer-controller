package events

import "testing"

func TestSafeLogValueProducesSingleRecord(t *testing.T) {
	if got := safeLogValue("first\r\nforged\nthird"); got != "first forged third" {
		t.Fatalf("unexpected safe log value: %q", got)
	}
}
