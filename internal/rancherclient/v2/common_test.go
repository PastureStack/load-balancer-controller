package client

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	utils "github.com/PastureStack/load-balancer-controller/utils"
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

	origin, err := newRequestOrigin(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := &RancherBaseClientImpl{Opts: &ClientOpts{AccessKey: "access", SecretKey: "secret"}, origin: origin}
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

	origin, err := newRequestOrigin(source.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := &RancherBaseClientImpl{Opts: &ClientOpts{AccessKey: "access", SecretKey: "secret", Timeout: time.Second}, origin: origin}
	err = client.doGet(source.URL, nil, &map[string]interface{}{})
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

func TestSchemaDiscoveryDoesNotSendCredentialsCrossOrigin(t *testing.T) {
	var reached atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached.Store(true)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-API-Schemas", target.URL+"/schemas")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer source.Close()
	_, err := NewRancherClient(&ClientOpts{Url: source.URL, AccessKey: "access", SecretKey: "secret"})
	if reached.Load() {
		t.Fatal("untrusted schema header sent an authenticated request to another origin")
	}
	if err == nil {
		t.Fatal("cross-origin schema discovery was accepted")
	}
}

func TestRequestOriginSeparatesAuthorityFromPath(t *testing.T) {
	origin, err := newRequestOrigin("https://API.example.test:443/custom/v2-beta")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		value string
		allow bool
	}{
		{"https://api.example.test/other/path?limit=1", true},
		{"https://api.example.test:0443/custom/v2-beta", true},
		{"/custom/projects", true},
		{"projects?limit=1", true},
		{"/%2f%2fevil.example/path", true},
		{"//evil.example/custom/v2-beta", false},
		{"https://api.example.test.evil.example/custom/v2-beta", false},
		{"https://api.example.test@evil.example/custom/v2-beta", false},
		{"https://api.example.test:444/custom/v2-beta", false},
		{"http://api.example.test:443/custom/v2-beta", false},
		{"https:opaque", false},
		{"file:///custom/v2-beta", false},
		{"https://api.example.test:%34%34%33/", false},
	} {
		t.Run(test.value, func(t *testing.T) {
			u, err := origin.resolve(test.value)
			if (err == nil) != test.allow {
				t.Fatalf("allow=%t err=%v", test.allow, err)
			}
			if err == nil && !strings.EqualFold(u.Hostname(), "api.example.test") {
				t.Fatal("path changed the request authority")
			}
		})
	}
	for _, root := range []string{"http://10.0.0.1:8080/prefix", "https://custom.internal/root", "http://[::1]:8080/v2-beta"} {
		origin, err := newRequestOrigin(root)
		if err != nil {
			t.Fatal("legitimate administrator root rejected:", err)
		}
		if _, err := origin.resolve(root); err != nil {
			t.Fatal(err)
		}
	}
	v6, err := newRequestOrigin("http://[::1]:8080/root")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v6.resolve("http://[0:0:0:0:0:0:0:1]:8080/other"); err != nil {
		t.Fatal("equivalent IPv6 host rejected:", err)
	}
	v4, err := newRequestOrigin("http://127.0.0.1:8080/root")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v4.resolve("http://[::ffff:127.0.0.1]:8080/root"); err == nil {
		t.Fatal("IPv4-mapped IPv6 authority was collapsed into the IPv4 Host")
	}
	zoned, err := newRequestOrigin("http://[fe80::1%25eth0]:8080/root")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := zoned.resolve("http://[fe80::1%25ETH0]:8080/root"); err == nil {
		t.Fatal("distinct IPv6 interface zone accepted")
	}
}

func TestAllAuthenticatedEntriesRejectCrossOriginAndKeepConstructorOrigin(t *testing.T) {
	var reached atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached.Add(1)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer target.Close()
	var authenticated atomic.Int32
	var source *httptest.Server
	source = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if !ok || user != "access" || password != "secret" {
			http.Error(w, "authentication missing", http.StatusUnauthorized)
			return
		}
		authenticated.Add(1)
		if r.URL.Path == "/custom/v2-beta" {
			w.Header().Set("X-API-Schemas", source.URL+"/custom/v2-beta")
			_, _ = w.Write([]byte(`{"data":[]}`))
			return
		}
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/finish", http.StatusFound)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer source.Close()
	client, err := NewRancherClient(&ClientOpts{Url: source.URL + "/custom/v2-beta", AccessKey: "access", SecretKey: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	base := client.RancherBaseClient.(*RancherBaseClientImpl)
	base.Types["project"] = Schema{Resource: Resource{Links: map[string]string{COLLECTION: target.URL}}, CollectionMethods: []string{"GET"}}
	resource := Resource{Links: map[string]string{"self": target.URL}, Actions: map[string]string{"activate": target.URL}}
	for name, call := range map[string]func() error{
		"get":        func() error { return base.doGet(target.URL, nil, &map[string]interface{}{}) },
		"delete":     func() error { return base.doDelete(target.URL) },
		"modify":     func() error { return base.doModify("POST", target.URL, nil, nil) },
		"link":       func() error { return base.GetLink(resource, "self", nil) },
		"pagination": func() error { return base.doNext(target.URL, nil) },
		"collection": func() error { return base.doList("project", nil, nil) },
		"action":     func() error { return base.doAction("project", "activate", &resource, nil, nil) },
		"websocket": func() error {
			_, _, err := base.Websocket("ws"+strings.TrimPrefix(target.URL, "http"), nil)
			return err
		},
		"websocket Host override": func() error {
			u, _ := url.Parse(target.URL)
			_, _, err := base.Websocket("ws"+strings.TrimPrefix(source.URL, "http"), map[string][]string{"Host": {u.Host}})
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := call(); err == nil || !strings.Contains(err.Error(), "cross-origin") {
				t.Fatal("cross-origin authenticated entry accepted:", err)
			}
		})
	}
	base.Opts.Url = target.URL // GetOpts exposes configuration, not the origin trust decision.
	if err := base.doGet(target.URL, nil, nil); err == nil {
		t.Fatal("mutable ClientOpts replaced the constructor origin")
	}
	if reached.Load() != 0 {
		t.Fatal("a cross-origin entry reached the target")
	}
	if err := base.doGet("/start", nil, &map[string]interface{}{}); err != nil {
		t.Fatal("same-origin relative path/redirect failed:", err)
	}
	if authenticated.Load() != 3 {
		t.Fatalf("authenticated requests=%d, want discovery and both redirect hops", authenticated.Load())
	}
}

func TestControlPlaneErrorSummaryOmitsPayloadAndRequestSecrets(t *testing.T) {
	apiError := &ApiError{StatusCode: 503, Status: "503 Unavailable\r\nforged\x1b", Msg: "secret response payload", Body: "secret response payload", Url: "https://user:password@example.test/?token=secret"}
	for _, err := range []error{apiError, &url.Error{Op: "Get", URL: apiError.Url, Err: apiError}} {
		text := utils.SanitizeLogText(ErrorSummary(err))
		if !strings.Contains(text, "503") || strings.ContainsAny(text, "\r\n\x1b") {
			t.Fatal("HTTP diagnosis lost or unsafe controls survived")
		}
		for _, secret := range []string{"payload", "secret", "password", "token="} {
			if strings.Contains(text, secret) {
				t.Fatal("error log exposed request/response material")
			}
		}
	}
	plain := errors.New("connection refused")
	if ErrorSummary(plain) != plain.Error() || ErrorSummary(nil) != "" {
		t.Fatal("ordinary error diagnosis changed")
	}
	requestError := &url.Error{Op: "Get", URL: apiError.Url, Err: plain}
	if actual := ErrorSummary(requestError); actual != "Get: connection refused" {
		t.Fatal("request URL was not omitted from the network diagnosis")
	}
}
