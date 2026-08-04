package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHandlerServesGinRoutesAndSwaggerDocs(t *testing.T) {
	t.Setenv("GIN_MODE", "test")

	app, err := NewApp(Config{
		ResourceRoot:   t.TempDir(),
		RequestTimeout: time.Second,
		AvatarTimeout:  time.Second,
		SessionTTL:     time.Minute,
		QRSessionTTL:   time.Minute,
	})
	if err != nil {
		t.Fatalf("NewApp() error = %v", err)
	}
	defer app.Close()

	handler := app.Handler()

	health := httptest.NewRecorder()
	handler.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/health", nil))
	if health.Code != http.StatusOK {
		t.Fatalf("GET /health status = %d", health.Code)
	}
	var healthBody struct {
		Code int            `json:"code"`
		Msg  string         `json:"msg"`
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(health.Body.Bytes(), &healthBody); err != nil {
		t.Fatalf("decode health JSON: %v", err)
	}
	if healthBody.Code != 0 || healthBody.Msg != "success" || healthBody.Data["ok"] != true {
		t.Fatalf("GET /health body = %#v", healthBody)
	}

	openapi := httptest.NewRecorder()
	handler.ServeHTTP(openapi, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	if openapi.Code != http.StatusOK {
		t.Fatalf("GET /openapi.json status = %d", openapi.Code)
	}
	var spec map[string]any
	if err := json.Unmarshal(openapi.Body.Bytes(), &spec); err != nil {
		t.Fatalf("decode OpenAPI JSON: %v", err)
	}
	if spec["openapi"] != "3.0.3" {
		t.Fatalf("openapi version = %v", spec["openapi"])
	}
	if _, ok := spec["code"]; ok {
		t.Fatalf("OpenAPI JSON should not be wrapped in API envelope")
	}
	paths, ok := spec["paths"].(map[string]any)
	if !ok {
		t.Fatalf("OpenAPI paths missing or invalid")
	}
	for _, path := range []string{"/wxapp/getCode", "/wxapp/getPhoneNumber", "/wxapp/operateWxData", "/accounts/avatar"} {
		if _, ok := paths[path]; !ok {
			t.Fatalf("OpenAPI path %s missing", path)
		}
	}
	for _, path := range []string{"/wxapp/getCode", "/wxapp/getPhoneNumber", "/wxapp/operateWxData"} {
		pathItem := paths[path].(map[string]any)
		post := pathItem["post"].(map[string]any)
		tags := post["tags"].([]any)
		if len(tags) != 1 || tags[0] != "wxapp" {
			t.Fatalf("OpenAPI path %s tags = %#v, want [wxapp]", path, tags)
		}
	}
	for _, path := range []string{"/accounts/{ref}", "/accounts/{ref}/getCode", "/accounts/{ref}/getPhoneNumber", "/accounts/{ref}/operateWxData", "/accounts/getCode", "/accounts/getPhoneNumber", "/accounts/operateWxData"} {
		if _, ok := paths[path]; ok {
			t.Fatalf("OpenAPI still exposes old account feature route %s", path)
		}
	}
	if _, ok := paths["/features"]; ok {
		t.Fatalf("OpenAPI still exposes /features")
	}

	docs := httptest.NewRecorder()
	handler.ServeHTTP(docs, httptest.NewRequest(http.MethodGet, "/docs", nil))
	if docs.Code != http.StatusMovedPermanently {
		t.Fatalf("GET /docs status = %d", docs.Code)
	}
	if got := docs.Header().Get("Location"); got != "/docs/index.html" {
		t.Fatalf("GET /docs Location = %q", got)
	}

	features := httptest.NewRecorder()
	handler.ServeHTTP(features, httptest.NewRequest(http.MethodGet, "/features", nil))
	if features.Code != http.StatusNotFound {
		t.Fatalf("GET /features status = %d", features.Code)
	}
	var notFoundBody struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data any    `json:"data"`
	}
	if err := json.Unmarshal(features.Body.Bytes(), &notFoundBody); err != nil {
		t.Fatalf("decode /features error JSON: %v", err)
	}
	if notFoundBody.Code == 0 || notFoundBody.Msg == "" || notFoundBody.Data != nil {
		t.Fatalf("GET /features body = %#v", notFoundBody)
	}

	oldPath := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/accounts/getCode", nil)
	req.Header.Set("Authorization", "Bearer "+currentAPIToken())
	handler.ServeHTTP(oldPath, req)
	if oldPath.Code != http.StatusNotFound {
		t.Fatalf("POST old account feature route status = %d", oldPath.Code)
	}
}

func newTestApp(t *testing.T) (*App, http.Handler) {
	t.Helper()
	t.Setenv("GIN_MODE", "test")
	t.Setenv("YYB_ADMIN_PASS", "")
	adminPass = "admin"
	adminPassFromEnv = false
	sessionsMu.Lock()
	sessions = map[string]sessionEntry{}
	sessionsMu.Unlock()
	app, err := NewApp(Config{
		ResourceRoot:   t.TempDir(),
		RequestTimeout: time.Second,
		AvatarTimeout:  time.Second,
		SessionTTL:     time.Minute,
		QRSessionTTL:   time.Minute,
	})
	if err != nil {
		t.Fatalf("NewApp() error = %v", err)
	}
	t.Cleanup(func() { _ = app.Close() })
	return app, app.Handler()
}

func postJSON(handler http.Handler, path, body string, cookie string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func mustLogin(t *testing.T, handler http.Handler) (string, string) {
	t.Helper()
	form := "username=admin&password=admin"
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("login status = %d, body = %s", rec.Code, rec.Body.String())
	}
	cookies := rec.Result().Cookies()
	var cookie string
	for _, c := range cookies {
		if c.Name == cookieName {
			cookie = c.Name + "=" + c.Value
		}
	}
	if cookie == "" {
		t.Fatal("login did not set session cookie")
	}
	return rec.Header().Get("Location"), cookie
}

func TestPasswordChangeAndLogoutFlow(t *testing.T) {
	_, handler := newTestApp(t)

	_, cookie := mustLogin(t, handler)

	// Wrong old password -> 400
	rec := postJSON(handler, "/account/password", `{"old_password":"wrong","new_password":"newpass123"}`, cookie)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("wrong old password status = %d", rec.Code)
	}

	// Empty new password -> 400
	rec = postJSON(handler, "/account/password", `{"old_password":"admin","new_password":""}`, cookie)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty new password status = %d", rec.Code)
	}

	// Same password -> 400
	rec = postJSON(handler, "/account/password", `{"old_password":"admin","new_password":"admin"}`, cookie)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("same password status = %d", rec.Code)
	}

	// Valid change -> 200
	rec = postJSON(handler, "/account/password", `{"old_password":"admin","new_password":"newpass123"}`, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("change password status = %d, body = %s", rec.Code, rec.Body.String())
	}

	// Old session invalidated after change -> accessing / redirects to login
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Cookie", cookie)
	home := httptest.NewRecorder()
	handler.ServeHTTP(home, req)
	if home.Code != http.StatusFound || home.Header().Get("Location") != "/login" {
		t.Fatalf("after password change, old session should redirect to login, got %d %q", home.Code, home.Header().Get("Location"))
	}

	// Login with old password should now fail
	form := "username=admin&password=admin"
	badLogin := httptest.NewRecorder()
	handler.ServeHTTP(badLogin, httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form)))
	if badLogin.Code != http.StatusFound || badLogin.Header().Get("Location") != "/login?err=1" {
		t.Fatalf("old password login should fail, got %d %q", badLogin.Code, badLogin.Header().Get("Location"))
	}

	// Login with new password succeeds
	form = "username=admin&password=newpass123"
	goodLogin := httptest.NewRecorder()
	handler.ServeHTTP(goodLogin, httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form)))
	if goodLogin.Code != http.StatusFound {
		t.Fatalf("new password login status = %d", goodLogin.Code)
	}
}

func TestLogoutInvalidatesSession(t *testing.T) {
	_, handler := newTestApp(t)

	_, cookie := mustLogin(t, handler)

	rec := postJSON(handler, "/account/logout", ``, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("logout status = %d", rec.Code)
	}
	// Session cookie cleared
	var setCookies []string
	for _, c := range rec.Result().Cookies() {
		if c.Name == cookieName {
			setCookies = append(setCookies, c.Name+"="+c.Value)
		}
	}
	if len(setCookies) == 0 || !strings.HasSuffix(setCookies[0], "=") {
		t.Fatalf("logout should clear cookie, got %v", setCookies)
	}

	// Old cookie should no longer authorize
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Cookie", cookie)
	home := httptest.NewRecorder()
	handler.ServeHTTP(home, req)
	if home.Code != http.StatusFound || home.Header().Get("Location") != "/login" {
		t.Fatalf("after logout, session should be invalid, got %d %q", home.Code, home.Header().Get("Location"))
	}
}
