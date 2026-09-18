package server

import (
	"fmt"
	"math"
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
		`id="token-form" method="post"`,
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
	tokenInput := regexp.MustCompile(`<input id="admin-token"[^>]*>`).FindString(body)
	if tokenInput == "" {
		t.Fatal("dashboard index does not contain the admin token input")
	}
	if strings.Contains(tokenInput, "name=") || strings.Contains(tokenInput, "minlength=") {
		t.Fatalf("admin token input can leak or mis-validate the token: %s", tokenInput)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	assertDashboardSecurityHeaders(t, response)
}

func TestDashboardClientGuardsAsyncState(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/assets/dashboard.js", nil)
	response := httptest.NewRecorder()
	newTestServer().Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	client := response.Body.String()
	for _, marker := range []string{
		"if (!isHeaderSafeToken(token))",
		"code < 0x21 || code > 0x7e",
		"const session = beginSession();",
		"request !== state.detailRequest",
		"setStatusButtonsBusy(state.statusUpdating)",
		"state.loading || state.cursorHistory.length === 0",
	} {
		if !strings.Contains(client, marker) {
			t.Fatalf("dashboard client does not contain state guard %q", marker)
		}
	}
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
	for _, marker := range []string{
		"/api/v1/meta",
		"/api/v1/demo/issues",
		`new URLSearchParams(window.location.search).get("demo")`,
		`searchParams.set("demo", "1")`,
		`searchParams.delete("demo")`,
		`metadata.demo_only === true`,
		`metadata.version === "string"`,
		`#build-version`,
	} {
		if !strings.Contains(dashboardScript.body, marker) {
			t.Fatalf("dashboard script does not contain demo marker %q", marker)
		}
	}
	for _, marker := range []string{
		`"zh-CN"`,
		"Intl.RelativeTimeFormat",
		"history.replaceState",
		"data-i18n",
		"next_cursor",
		"cursorHistory",
	} {
		if !strings.Contains(dashboardScript.body, marker) {
			t.Fatalf("dashboard script does not contain localization marker %q", marker)
		}
	}
	if strings.Contains(dashboardScript.body, "offset: String(state.offset)") {
		t.Fatal("dashboard still uses offset pagination")
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

func TestDashboardExposesLandmarksAndLiveRegions(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()
	newTestServer().Handler().ServeHTTP(response, request)
	body := response.Body.String()

	// The masthead used to sit inside <main>, so the document had no banner
	// landmark and the brand, version, language selector and connection state
	// were all inside the main landmark.
	mastheadIndex := strings.Index(body, `<header class="masthead">`)
	mainIndex := strings.Index(body, "<main>")
	if mastheadIndex < 0 || mainIndex < 0 {
		t.Fatalf("masthead at %d, main at %d, want both present", mastheadIndex, mainIndex)
	}
	if mastheadIndex > mainIndex {
		t.Fatal("the masthead must precede <main> so it forms a banner landmark")
	}

	// renderPage rewrites both on every filter change and page step; only the
	// login and workspace messages used to announce anything.
	for _, marker := range []string{
		`id="result-copy" role="status"`,
		`id="page-copy" role="status"`,
	} {
		if !strings.Contains(body, marker) {
			t.Fatalf("dashboard index does not contain %q", marker)
		}
	}
}

func TestDashboardStylesMeetTextContrast(t *testing.T) {
	// Every --subtle usage is 10-12px, so WCAG AA wants 4.5:1 with no
	// large-text relief. #64748b reached 3.90:1 on the panel surface, and the
	// token placeholder's #475569 only 2.45:1.
	response := httptest.NewRecorder()
	newTestServer().Handler().ServeHTTP(
		response, httptest.NewRequest(http.MethodGet, "/assets/dashboard.css", nil),
	)
	css := response.Body.String()

	subtle := regexp.MustCompile(`--subtle:\s*(#[0-9a-fA-F]{6})`).FindStringSubmatch(css)
	if subtle == nil {
		t.Fatal("dashboard.css does not declare --subtle")
	}
	page := parseHexColor(t, "#070a12")
	panel := blendColor(parseHexColor(t, "#0f1523"), 0.82, page)
	for name, background := range map[string][3]float64{"panel": panel, "page": page} {
		if got := contrastRatio(parseHexColor(t, subtle[1]), background); got < 4.5 {
			t.Fatalf("--subtle %s on the %s = %.2f:1, want at least 4.5:1", subtle[1], name, got)
		}
	}
	if strings.Contains(css, "#475569") {
		t.Fatal("dashboard.css still uses #475569, which is 2.45:1 against the panel")
	}
}

func parseHexColor(t *testing.T, value string) [3]float64 {
	t.Helper()
	var channels [3]float64
	for index := 0; index < 3; index++ {
		var component int
		if _, err := fmt.Sscanf(value[1+index*2:3+index*2], "%02x", &component); err != nil {
			t.Fatalf("parse %q: %v", value, err)
		}
		channels[index] = float64(component)
	}
	return channels
}

func blendColor(foreground [3]float64, alpha float64, background [3]float64) [3]float64 {
	var blended [3]float64
	for index := range blended {
		blended[index] = foreground[index]*alpha + background[index]*(1-alpha)
	}
	return blended
}

// contrastRatio implements the WCAG 2.1 relative luminance formula.
func contrastRatio(first, second [3]float64) float64 {
	luminance := func(color [3]float64) float64 {
		weights := [3]float64{0.2126, 0.7152, 0.0722}
		total := 0.0
		for index, channel := range color {
			value := channel / 255
			if value <= 0.04045 {
				value /= 12.92
			} else {
				value = math.Pow((value+0.055)/1.055, 2.4)
			}
			total += weights[index] * value
		}
		return total
	}
	lighter, darker := luminance(first), luminance(second)
	if lighter < darker {
		lighter, darker = darker, lighter
	}
	return (lighter + 0.05) / (darker + 0.05)
}
