package telemetry

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIngestionAuthentication(t *testing.T) {
	paths := []string{"/v1/measurements", "/v1/endpoint-telemetry", "/v1/endpoint-diagnostics", "/v1/application-experience", "/v1/route-decisions"}
	for _, path := range paths {
		for _, authorization := range []string{"", "Bearer wrong", "Basic secret", "Bearer secret"} {
			t.Run(path+"/"+authorization, func(t *testing.T) {
				request := httptest.NewRequest(http.MethodPost, path, strings.NewReader("invalid JSON"))
				request.Header.Set("Authorization", authorization)
				response := httptest.NewRecorder()
				NewAuthenticatedHandler(nil, "secret").ServeHTTP(response, request)
				expected := http.StatusUnauthorized
				if authorization == "Bearer secret" {
					expected = http.StatusBadRequest
				}
				if response.Code != expected {
					t.Fatalf("status = %d, want %d", response.Code, expected)
				}
			})
		}
	}
}

func TestIngestionAuthenticationCompatibility(t *testing.T) {
	for _, token := range []string{"", "secret"} {
		response := httptest.NewRecorder()
		NewAuthenticatedHandler(nil, token).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))
		if response.Code != http.StatusNoContent {
			t.Fatalf("health status = %d", response.Code)
		}
	}
	response := httptest.NewRecorder()
	NewAuthenticatedHandler(nil, "").ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/measurements", strings.NewReader("invalid JSON")))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("legacy status = %d", response.Code)
	}
}
