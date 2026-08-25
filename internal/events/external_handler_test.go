package events

import (
	"reflect"
	"testing"

	client "github.com/PastureStack/load-balancer-controller/internal/rancherclient/v2"
)

func TestRemoveExternalHandlersDeduplicatesPreviousNames(t *testing.T) {
	original := removeOldHandler
	defer func() {
		removeOldHandler = original
	}()

	var removed []string
	removeOldHandler = func(name string, _ *client.RancherClient) error {
		removed = append(removed, name)
		return nil
	}

	router := &EventRouter{name: "compose-executor"}
	if err := router.RemoveExternalHandlers(
		"",
		"previous-compose-executor",
		"previous-compose-executor",
		"compose-executor",
	); err != nil {
		t.Fatal(err)
	}

	want := []string{"previous-compose-executor"}
	if !reflect.DeepEqual(removed, want) {
		t.Fatalf("removed = %#v, want %#v", removed, want)
	}
}
