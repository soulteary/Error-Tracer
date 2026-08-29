package healthcheck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCheckRequestsReadiness(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			t.Errorf("method = %q, want GET", request.Method)
		}
		if request.URL.Path != "/readyz" {
			t.Errorf("path = %q, want /readyz", request.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	}))
	t.Cleanup(server.Close)

	address := strings.TrimPrefix(server.URL, "http://")
	if err := Check(context.Background(), address); err != nil {
		t.Fatalf("Check() error = %v", err)
	}
}

func TestCheckRejectsUnreadyAndRedirectResponses(t *testing.T) {
	for _, status := range []int{http.StatusServiceUnavailable, http.StatusFound} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if status == http.StatusFound {
					w.Header().Set("Location", "/readyz")
				}
				w.WriteHeader(status)
			}))
			t.Cleanup(server.Close)

			if err := Check(
				context.Background(), strings.TrimPrefix(server.URL, "http://"),
			); err == nil {
				t.Fatalf("Check() error = nil for HTTP %d", status)
			}
		})
	}
}

func TestCheckHonorsCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Check(ctx, "127.0.0.1:8080"); err == nil {
		t.Fatal("Check() error = nil for cancelled context")
	}
}

func TestTargetURL(t *testing.T) {
	tests := map[string]string{
		"":                "http://127.0.0.1:8080/readyz",
		":9090":           "http://127.0.0.1:9090/readyz",
		"0.0.0.0:9090":    "http://127.0.0.1:9090/readyz",
		"[::]:9090":       "http://[::1]:9090/readyz",
		"localhost:9090":  "http://localhost:9090/readyz",
		"192.0.2.10:9090": "http://192.0.2.10:9090/readyz",
	}
	for input, want := range tests {
		got, err := targetURL(input)
		if err != nil {
			t.Fatalf("targetURL(%q) error = %v", input, err)
		}
		if got != want {
			t.Errorf("targetURL(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestTargetURLRejectsInvalidAddress(t *testing.T) {
	for _, address := range []string{"localhost", "127.0.0.1", ":not-a-port", "http://localhost:8080"} {
		if _, err := targetURL(address); err == nil {
			t.Errorf("targetURL(%q) error = nil", address)
		}
	}
}
