package client

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestRancherClientSchemaAndListTransport(t *testing.T) {
	var authenticated atomic.Int32
	queries := make(chan string, 1)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		user, password, ok := request.BasicAuth()
		if !ok || user != "access" || password != "secret" {
			http.Error(response, "unauthorized", http.StatusUnauthorized)
			return
		}
		authenticated.Add(1)
		switch request.URL.Path {
		case "/v2-beta":
			response.Header().Set("X-API-Schemas", server.URL+"/v2-beta")
			_, _ = fmt.Fprintf(response, `{"data":[{"id":"project","collectionMethods":["GET"],"links":{"collection":%q}}]}`, server.URL+"/v2-beta/projects")
		case "/v2-beta/projects":
			queries <- request.URL.RawQuery
			_, _ = response.Write([]byte(`{"data":[{"id":"project-1"}]}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	client, err := NewRancherClient(&ClientOpts{Url: server.URL, AccessKey: "access", SecretKey: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	projects, err := client.Project.List(&ListOpts{Filters: map[string]interface{}{
		"name":  "pasture",
		"label": []string{"blue", "green"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(projects.Data) != 1 || projects.Data[0].Id != "project-1" {
		t.Fatalf("unexpected project response: %#v", projects.Data)
	}
	values, err := url.ParseQuery(<-queries)
	if err != nil {
		t.Fatal(err)
	}
	if values.Get("name") != "pasture" || strings.Join(values["label"], ",") != "blue,green" {
		t.Fatalf("unexpected filters: %#v", values)
	}
	if authenticated.Load() != 2 {
		t.Fatalf("authenticated requests=%d, want 2", authenticated.Load())
	}
}

func TestWebsocketForwardsHeadersAndBasicAuth(t *testing.T) {
	type observedHeaders struct {
		user, password, trace string
		basicOK               bool
	}
	observed := make(chan observedHeaders, 1)
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		user, password, ok := request.BasicAuth()
		observed <- observedHeaders{user: user, password: password, trace: request.Header.Get("X-Trace-ID"), basicOK: ok}
		connection, err := upgrader.Upgrade(response, request, nil)
		if err == nil {
			_ = connection.Close()
		}
	}))
	defer server.Close()

	client := &RancherBaseClientImpl{Opts: &ClientOpts{AccessKey: "access", SecretKey: "secret"}}
	connection, _, err := client.Websocket("ws"+strings.TrimPrefix(server.URL, "http"), map[string][]string{"X-Trace-ID": {"trace-1"}})
	if err != nil {
		t.Fatal(err)
	}
	_ = connection.Close()

	headers := <-observed
	if !headers.basicOK || headers.user != "access" || headers.password != "secret" || headers.trace != "trace-1" {
		t.Fatalf("unexpected websocket headers: %#v", headers)
	}
}

func TestHTTPClientBlocksCrossOriginRedirect(t *testing.T) {
	var targetReached atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		targetReached.Store(true)
		_, _ = response.Write([]byte(`{}`))
	}))
	defer target.Close()

	source := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		http.Redirect(response, request, target.URL, http.StatusFound)
	}))
	defer source.Close()

	client := &RancherBaseClientImpl{Opts: &ClientOpts{AccessKey: "access", SecretKey: "secret", Timeout: time.Second}}
	err := client.doGet(source.URL, nil, &map[string]interface{}{})
	if err == nil || !strings.Contains(err.Error(), "cross-origin redirect blocked") {
		t.Fatalf("unexpected redirect result: %v", err)
	}
	if targetReached.Load() {
		t.Fatal("cross-origin redirect reached the target")
	}
}

func TestSafeURLForLogRemovesSecrets(t *testing.T) {
	value := safeURLForLog("https://user:password@example.test/path?token=secret#fragment")
	for _, secret := range []string{"password", "token", "secret", "fragment"} {
		if strings.Contains(value, secret) {
			t.Fatalf("safe URL still contains %q: %s", secret, value)
		}
	}
}
