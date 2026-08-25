package events

import (
	"testing"

	"github.com/PastureStack/load-balancer-controller/internal/rancherclient/v2"
)

func TestNewEventRouterBuildsBoundedSubscriptionURL(t *testing.T) {
	router, err := NewEventRouter(
		"compose", 1, "https://api.example.test/v2-beta?token=secret#fragment",
		"access", "secret", &client.RancherClient{}, nil, "resource", 1, PingConfig{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if router.subscribeURL != "wss://api.example.test/v2-beta/subscribe" {
		t.Fatalf("unexpected subscription URL: %s", router.subscribeURL)
	}
}

func TestNewEventRouterRejectsUnsupportedScheme(t *testing.T) {
	_, err := NewEventRouter(
		"compose", 1, "file:///tmp/socket", "", "", &client.RancherClient{}, nil, "resource", 1, PingConfig{},
	)
	if err == nil {
		t.Fatal("expected unsupported event API URL scheme to fail")
	}
}
