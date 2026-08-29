package server

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func TestDashboardIndex(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()

	newTestServer().Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	body := response.Body.String()
	for _, marker := range []string{
		"<title>Error-Tracer</title>",
		`id="token-form"`,
		`id="language-select"`,
		`id="demo-button"`,
		`id="demo-banner"`,
		`id="issue-list"`,
		`src="/assets/dashboard.js"`,
		`href="/assets/dashboard.css"`,
	} {
		if !strings.Contains(body, marker) {
			t.Fatalf("dashboard index does not contain %q", marker)
		}
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	assertDashboardSecurityHeaders(t, response)
}

func TestDashboardAssets(t *testing.T) {
	tests := []struct {
		path        string
		contentType string
		marker      string
	}{
		{path: "/assets/dashboard.css", contentType: "text/css; charset=utf-8", marker: ":root"},
		{path: "/assets/dashboard.js", contentType: "text/javascript; charset=utf-8", marker: "requestJSON"},
	}

	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			response := httptest.NewRecorder()
			newTestServer().Handler().ServeHTTP(response, request)

			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
			}
			if got := response.Header().Get("Content-Type"); got != test.contentType {
				t.Fatalf("Content-Type = %q, want %q", got, test.contentType)
			}
			if got := response.Header().Get("Cache-Control"); got != "public, max-age=300" {
				t.Fatalf("Cache-Control = %q", got)
			}
			if got := response.Header().Get("ETag"); got == "" {
				t.Fatal("ETag is empty")
			}
			if !strings.Contains(response.Body.String(), test.marker) {
				t.Fatalf("asset does not contain %q", test.marker)
			}
			assertDashboardSecurityHeaders(t, response)

			conditional := httptest.NewRequest(http.MethodGet, test.path, nil)
			conditional.Header.Set("If-None-Match", response.Header().Get("ETag"))
			notModified := httptest.NewRecorder()
			newTestServer().Handler().ServeHTTP(notModified, conditional)
			if notModified.Code != http.StatusNotModified {
				t.Fatalf("conditional status = %d, want %d", notModified.Code, http.StatusNotModified)
			}
			if notModified.Body.Len() != 0 {
				t.Fatalf("conditional body length = %d, want 0", notModified.Body.Len())
			}
		})
	}
}

func TestDashboardRoutesAreExactAndReadOnly(t *testing.T) {
	tests := []struct {
		method string
		path   string
		want   int
	}{
		{method: http.MethodPost, path: "/", want: http.StatusMethodNotAllowed},
		{method: http.MethodPost, path: "/assets/dashboard.js", want: http.StatusMethodNotAllowed},
		{method: http.MethodGet, path: "/not-a-dashboard-route", want: http.StatusNotFound},
	}
	for _, test := range tests {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(test.method, test.path, nil)
		newTestServer().Handler().ServeHTTP(response, request)
		if response.Code != test.want {
			t.Errorf("%s %s: status = %d, want %d", test.method, test.path, response.Code, test.want)
		}
	}
}

func TestDashboardScriptAvoidsCredentialPersistenceAndHTMLInjection(t *testing.T) {
	for _, forbidden := range []string{
		"localStorage",
		"sessionStorage",
		"document.cookie",
		"innerHTML",
		"outerHTML",
		"insertAdjacentHTML",
	} {
		if strings.Contains(dashboardScript.body, forbidden) {
			t.Fatalf("dashboard script contains forbidden browser API %q", forbidden)
		}
	}
	if !strings.Contains(dashboardScript.body, "textContent") {
		t.Fatal("dashboard script does not use textContent for rendering")
	}
	for _, marker := range []string{"/api/v1/meta", "/api/v1/demo/issues"} {
		if !strings.Contains(dashboardScript.body, marker) {
			t.Fatalf("dashboard script does not contain demo marker %q", marker)
		}
	}
	for _, marker := range []string{
		`"zh-CN"`,
		"Intl.RelativeTimeFormat",
		"history.replaceState",
		"data-i18n",
	} {
		if !strings.Contains(dashboardScript.body, marker) {
			t.Fatalf("dashboard script does not contain localization marker %q", marker)
		}
	}
}

func TestDashboardTranslationCatalogsStayAligned(t *testing.T) {
	body := dashboardScript.body
	englishStart := strings.Index(body, "    en: {")
	chineseStart := strings.Index(body, "    \"zh-CN\": {")
	if englishStart < 0 || chineseStart < 0 || chineseStart <= englishStart {
		t.Fatal("dashboard translation catalogs are missing")
	}
	chineseEndOffset := strings.Index(body[chineseStart:], "\n    },\n  };")
	if chineseEndOffset < 0 {
		t.Fatal("Simplified Chinese translation catalog is not terminated")
	}

	keyPattern := regexp.MustCompile(`(?m)^      "([^"]+)":`)
	keys := func(section string) map[string]bool {
		result := make(map[string]bool)
		for _, match := range keyPattern.FindAllStringSubmatch(section, -1) {
			result[match[1]] = true
		}
		return result
	}
	english := keys(body[englishStart:chineseStart])
	chinese := keys(body[chineseStart : chineseStart+chineseEndOffset])
	if len(english) < 80 || len(chinese) != len(english) {
		t.Fatalf("translation key counts = en:%d zh-CN:%d", len(english), len(chinese))
	}
	for key := range english {
		if !chinese[key] {
			t.Errorf("Simplified Chinese catalog is missing %q", key)
		}
	}
	for key := range chinese {
		if !english[key] {
			t.Errorf("English catalog is missing %q", key)
		}
	}

	attributePattern := regexp.MustCompile(`data-i18n(?:-placeholder|-aria-label)?="([^"]+)"`)
	for _, match := range attributePattern.FindAllStringSubmatch(dashboardIndex.body, -1) {
		if !english[match[1]] {
			t.Errorf("dashboard markup references unknown translation key %q", match[1])
		}
	}
}

func assertDashboardSecurityHeaders(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	csp := response.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "default-src 'none'") ||
		!strings.Contains(csp, "frame-ancestors 'none'") {
		t.Fatalf("Content-Security-Policy = %q", csp)
	}
	if strings.Contains(csp, "unsafe-inline") || strings.Contains(csp, "unsafe-eval") {
		t.Fatalf("Content-Security-Policy permits unsafe execution: %q", csp)
	}
	if got := response.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q", got)
	}
	if got := response.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Fatalf("X-Frame-Options = %q", got)
	}
	if got := response.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Fatalf("Referrer-Policy = %q", got)
	}
}
