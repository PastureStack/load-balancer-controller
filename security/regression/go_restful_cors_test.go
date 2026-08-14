package regression_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/emicklei/go-restful"
)

func TestGoRestfulCORSRequiresTheWholeOriginToMatch(t *testing.T) {
	allowedPattern := `https://([a-z0-9-]+\.)?example\.com`

	tests := []struct {
		name        string
		origin      string
		wantAllowed bool
	}{
		{
			name:        "exact domain",
			origin:      "https://example.com",
			wantAllowed: true,
		},
		{
			name:        "intended subdomain",
			origin:      "https://console.example.com",
			wantAllowed: true,
		},
		{
			name:        "attacker controlled suffix",
			origin:      "https://example.com.attacker.invalid",
			wantAllowed: false,
		},
		{
			name:        "attacker controlled prefix",
			origin:      "https://attacker.invalid/https://example.com",
			wantAllowed: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := serveCORSRequest(t, allowedPattern, test.origin)
			allowedOrigin := response.Header().Get(restful.HEADER_AccessControlAllowOrigin)

			if test.wantAllowed && allowedOrigin != test.origin {
				t.Fatalf("expected origin %q to be allowed, got header %q", test.origin, allowedOrigin)
			}
			if !test.wantAllowed && allowedOrigin != "" {
				t.Fatalf("expected origin %q to be rejected, got header %q", test.origin, allowedOrigin)
			}
		})
	}
}

func serveCORSRequest(t *testing.T, allowedPattern, origin string) *httptest.ResponseRecorder {
	t.Helper()

	container := restful.NewContainer()
	service := new(restful.WebService)
	service.Path("/")
	service.Route(service.GET("/").To(func(_ *restful.Request, response *restful.Response) {
		response.WriteHeader(http.StatusNoContent)
	}))
	container.Add(service)

	cors := restful.CrossOriginResourceSharing{
		AllowedDomains: []string{allowedPattern},
	}
	container.Filter(cors.Filter)

	request := httptest.NewRequest(http.MethodGet, "http://service.invalid/", nil)
	request.Header.Set(restful.HEADER_Origin, origin)
	recorder := httptest.NewRecorder()
	container.ServeHTTP(recorder, request)

	return recorder
}
