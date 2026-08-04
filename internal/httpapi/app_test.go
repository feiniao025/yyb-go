package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
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

func TestRegisterCreatesUserWithIsolatedToken(t *testing.T) {
	app, handler := newTestApp(t)

	rec := postJSON(handler, "/register", `{"username":"alice","password":"secret123"}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("register status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Code int `json:"code"`
		Data struct {
			Username string `json:"username"`
			APIToken string `json:"api_token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode register JSON: %v", err)
	}
	if envelope.Code != 0 || envelope.Data.Username != "alice" || envelope.Data.APIToken == "" {
		t.Fatalf("register body = %#v", envelope)
	}
	aliceToken := envelope.Data.APIToken

	// Duplicate username -> 409
	rec = postJSON(handler, "/register", `{"username":"ALICE","password":"another1"}`, "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate register status = %d, body = %s", rec.Code, rec.Body.String())
	}

	// Missing fields -> 400
	rec = postJSON(handler, "/register", `{"username":"bob"}`, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing password status = %d", rec.Code)
	}

	// Admin username cannot be registered -> 409
	rec = postJSON(handler, "/register", `{"username":"admin","password":"secret123"}`, "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("register admin status = %d", rec.Code)
	}

	// Alice's token works for authenticated API and differs from admin token
	adminTokRec := httptest.NewRecorder()
	adminReq := httptest.NewRequest(http.MethodGet, "/token", nil)
	adminReq.Header.Set("Authorization", "Bearer "+currentAPIToken())
	handler.ServeHTTP(adminTokRec, adminReq)
	var adminTok struct {
		Code int `json:"code"`
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	_ = json.Unmarshal(adminTokRec.Body.Bytes(), &adminTok)
	if adminTok.Data.Token == "" {
		t.Fatalf("admin token empty, body = %s", adminTokRec.Body.String())
	}
	if adminTok.Data.Token == aliceToken {
		t.Fatalf("user token should differ from admin token")
	}

	aliceTokRec := httptest.NewRecorder()
	aliceReq := httptest.NewRequest(http.MethodGet, "/token", nil)
	aliceReq.Header.Set("Authorization", "Bearer "+aliceToken)
	handler.ServeHTTP(aliceTokRec, aliceReq)
	if aliceTokRec.Code != http.StatusOK {
		t.Fatalf("alice /token status = %d, body = %s", aliceTokRec.Code, aliceTokRec.Body.String())
	}

	// Invalid token -> 401
	badRec := httptest.NewRecorder()
	badReq := httptest.NewRequest(http.MethodGet, "/accounts", nil)
	badReq.Header.Set("Authorization", "Bearer not-a-real-token")
	handler.ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusUnauthorized {
		t.Fatalf("invalid token status = %d", badRec.Code)
	}
	_ = app
}

func TestRegisterUserLoginAndRotateToken(t *testing.T) {
	_, handler := newTestApp(t)

	rec := postJSON(handler, "/register", `{"username":"carol","password":"secret123"}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("register status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Data struct {
			APIToken string `json:"api_token"`
		} `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &envelope)
	oldToken := envelope.Data.APIToken

	// User login (form) -> session cookie
	form := "username=carol&password=secret123"
	loginReq := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form))
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusFound {
		t.Fatalf("user login status = %d, body = %s", loginRec.Code, loginRec.Body.String())
	}
	var cookie string
	for _, c := range loginRec.Result().Cookies() {
		if c.Name == cookieName {
			cookie = c.Name + "=" + c.Value
		}
	}
	if cookie == "" {
		t.Fatal("user login did not set session cookie")
	}

	// Wrong password login -> redirect with err
	badForm := "username=carol&password=wrongpass"
	badLogin := httptest.NewRecorder()
	handler.ServeHTTP(badLogin, httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(badForm)))
	if badLogin.Code != http.StatusFound || badLogin.Header().Get("Location") != "/login?err=1" {
		t.Fatalf("wrong password login should fail, got %d %q", badLogin.Code, badLogin.Header().Get("Location"))
	}

	// Rotate token via session
	rotateRec := postJSON(handler, "/token/rotate", ``, cookie)
	if rotateRec.Code != http.StatusOK {
		t.Fatalf("rotate status = %d, body = %s", rotateRec.Code, rotateRec.Body.String())
	}
	var rotated struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	_ = json.Unmarshal(rotateRec.Body.Bytes(), &rotated)
	if rotated.Data.Token == "" || rotated.Data.Token == oldToken {
		t.Fatalf("rotate should return new token, got %q", rotated.Data.Token)
	}

	// Old token now invalid
	oldRec := httptest.NewRecorder()
	oldReq := httptest.NewRequest(http.MethodGet, "/accounts", nil)
	oldReq.Header.Set("Authorization", "Bearer "+oldToken)
	handler.ServeHTTP(oldRec, oldReq)
	if oldRec.Code != http.StatusUnauthorized {
		t.Fatalf("old token should be invalid, got %d", oldRec.Code)
	}
}

func TestUserDataIsolation(t *testing.T) {
	app, handler := newTestApp(t)

	// Register two users
	recA := postJSON(handler, "/register", `{"username":"userA","password":"secret123"}`, "")
	if recA.Code != http.StatusOK {
		t.Fatalf("register A status = %d", recA.Code)
	}
	var envA struct {
		Data struct {
			APIToken string `json:"api_token"`
		} `json:"data"`
	}
	_ = json.Unmarshal(recA.Body.Bytes(), &envA)

	recB := postJSON(handler, "/register", `{"username":"userB","password":"secret123"}`, "")
	if recB.Code != http.StatusOK {
		t.Fatalf("register B status = %d", recB.Code)
	}
	var envB struct {
		Data struct {
			APIToken string `json:"api_token"`
		} `json:"data"`
	}
	_ = json.Unmarshal(recB.Body.Bytes(), &envB)

	uA, err := app.db.GetUserByUsername(context.Background(), "userA")
	if err != nil {
		t.Fatalf("get userA: %v", err)
	}
	uB, err := app.db.GetUserByUsername(context.Background(), "userB")
	if err != nil {
		t.Fatalf("get userB: %v", err)
	}
	nickA := "a-account"
	nickB := "b-account"
	status := "alive"
	if _, err = app.db.UpsertAccount(context.Background(), "openid-a", "buf", &nickA, &nickA, nil, nil, nil, &status, &uA.ID); err != nil {
		t.Fatalf("upsert account A: %v", err)
	}
	if _, err = app.db.UpsertAccount(context.Background(), "openid-b", "buf", &nickB, &nickB, nil, nil, nil, &status, &uB.ID); err != nil {
		t.Fatalf("upsert account B: %v", err)
	}

	getAccounts := func(token string) ([]map[string]any, int) {
		req := httptest.NewRequest(http.MethodGet, "/accounts", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		var out struct {
			Code int            `json:"code"`
			Data []map[string]any `json:"data"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return out.Data, rec.Code
	}

	// A sees only its own account
	acctsA, code := getAccounts(envA.Data.APIToken)
	if code != http.StatusOK || len(acctsA) != 1 || acctsA[0]["openid"] != "openid-a" {
		t.Fatalf("A accounts = %#v (code %d)", acctsA, code)
	}
	// B sees only its own account
	acctsB, code := getAccounts(envB.Data.APIToken)
	if code != http.StatusOK || len(acctsB) != 1 || acctsB[0]["openid"] != "openid-b" {
		t.Fatalf("B accounts = %#v (code %d)", acctsB, code)
	}
	// Admin sees both
	adminTok := currentAPIToken()
	acctsAdmin, code := getAccounts(adminTok)
	if code != http.StatusOK || len(acctsAdmin) != 2 {
		t.Fatalf("admin accounts = %#v (code %d)", acctsAdmin, code)
	}

	// A cannot operate B's account -> 404
	req := httptest.NewRequest(http.MethodPost, "/accounts/refresh", strings.NewReader(`{"ref":"openid-b"}`))
	req.Header.Set("Authorization", "Bearer "+envA.Data.APIToken)
	refreshRec := httptest.NewRecorder()
	handler.ServeHTTP(refreshRec, req)
	if refreshRec.Code != http.StatusNotFound {
		t.Fatalf("A refreshing B account status = %d, body = %s", refreshRec.Code, refreshRec.Body.String())
	}
}

func registerUser(t *testing.T, handler http.Handler, username, password string) (string, int64) {
	t.Helper()
	rec := postJSON(handler, "/register", `{"username":"`+username+`","password":"`+password+`"}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("register %s status = %d, body = %s", username, rec.Code, rec.Body.String())
	}
	var env struct {
		Code int `json:"code"`
		Data struct {
			Username string `json:"username"`
			APIToken string `json:"api_token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode register: %v", err)
	}
	app := currentTestApp()
	u, err := app.db.GetUserByUsername(context.Background(), username)
	if err != nil {
		t.Fatalf("get user %s: %v", username, err)
	}
	return env.Data.APIToken, u.ID
}

var currentAppRef *App

func currentTestApp() *App {
	return currentAppRef
}

func TestAdminUserManagement(t *testing.T) {
	app, handler := newTestApp(t)
	currentAppRef = app
	defer func() { currentAppRef = nil }()

	tokenA, idA := registerUser(t, handler, "mgmtA", "secret123")
	tokenB, idB := registerUser(t, handler, "mgmtB", "secret123")

	adminReq := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+currentAPIToken())
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	// Admin lists users
	listRec := adminReq(http.MethodGet, "/admin/users", "")
	if listRec.Code != http.StatusOK {
		t.Fatalf("admin list users status = %d", listRec.Code)
	}
	var listEnv struct {
		Data []struct {
			ID       int64  `json:"id"`
			Username string `json:"username"`
			Disabled bool   `json:"disabled"`
		} `json:"data"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listEnv); err != nil {
		t.Fatalf("decode list users: %v", err)
	}
	if len(listEnv.Data) != 2 {
		t.Fatalf("admin list users len = %d, want 2", len(listEnv.Data))
	}

	// Regular user cannot access admin endpoints -> 403
	req := httptest.NewRequest(http.MethodGet, "/admin/users", nil)
	req.Header.Set("Authorization", "Bearer "+tokenB)
	forbidden := httptest.NewRecorder()
	handler.ServeHTTP(forbidden, req)
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("regular user admin list status = %d, want 403", forbidden.Code)
	}

	// Disable user A
	disableRec := adminReq(http.MethodPost, "/admin/users/"+strconv.FormatInt(idA, 10)+"/disable", "")
	if disableRec.Code != http.StatusOK {
		t.Fatalf("disable status = %d, body = %s", disableRec.Code, disableRec.Body.String())
	}

	// A's token now invalid
	req = httptest.NewRequest(http.MethodGet, "/accounts", nil)
	req.Header.Set("Authorization", "Bearer "+tokenA)
	recA := httptest.NewRecorder()
	handler.ServeHTTP(recA, req)
	if recA.Code != http.StatusUnauthorized {
		t.Fatalf("disabled user token status = %d, want 401", recA.Code)
	}

	// A cannot login
	form := "username=mgmtA&password=secret123"
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form)))
	if loginRec.Code != http.StatusFound || loginRec.Header().Get("Location") != "/login?err=1" {
		t.Fatalf("disabled user login = %d %q", loginRec.Code, loginRec.Header().Get("Location"))
	}

	// Enable user A
	enableRec := adminReq(http.MethodPost, "/admin/users/"+strconv.FormatInt(idA, 10)+"/enable", "")
	if enableRec.Code != http.StatusOK {
		t.Fatalf("enable status = %d", enableRec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/accounts", nil)
	req.Header.Set("Authorization", "Bearer "+tokenA)
	recA2 := httptest.NewRecorder()
	handler.ServeHTTP(recA2, req)
	if recA2.Code != http.StatusOK {
		t.Fatalf("enabled user token status = %d, want 200", recA2.Code)
	}

	// Reset password: token kept, old password invalid
	resetRec := adminReq(http.MethodPost, "/admin/users/"+strconv.FormatInt(idA, 10)+"/password", `{"new_password":"newsecret1"}`)
	if resetRec.Code != http.StatusOK {
		t.Fatalf("reset password status = %d, body = %s", resetRec.Code, resetRec.Body.String())
	}
	// old password login fails
	badLogin := httptest.NewRecorder()
	handler.ServeHTTP(badLogin, httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form)))
	if badLogin.Code != http.StatusFound || badLogin.Header().Get("Location") != "/login?err=1" {
		t.Fatalf("old password after reset = %d %q", badLogin.Code, badLogin.Header().Get("Location"))
	}
	// token still valid
	req = httptest.NewRequest(http.MethodGet, "/accounts", nil)
	req.Header.Set("Authorization", "Bearer "+tokenA)
	recA3 := httptest.NewRecorder()
	handler.ServeHTTP(recA3, req)
	if recA3.Code != http.StatusOK {
		t.Fatalf("token after reset status = %d, want 200", recA3.Code)
	}

	// Delete user B
	delRec := adminReq(http.MethodDelete, "/admin/users/"+strconv.FormatInt(idB, 10), "")
	if delRec.Code != http.StatusOK {
		t.Fatalf("delete status = %d, body = %s", delRec.Code, delRec.Body.String())
	}
	// B's token invalid
	req = httptest.NewRequest(http.MethodGet, "/accounts", nil)
	req.Header.Set("Authorization", "Bearer "+tokenB)
	recB := httptest.NewRecorder()
	handler.ServeHTTP(recB, req)
	if recB.Code != http.StatusUnauthorized {
		t.Fatalf("deleted user token status = %d, want 401", recB.Code)
	}
	// username reusable
	reReg := postJSON(handler, "/register", `{"username":"mgmtB","password":"secret456"}`, "")
	if reReg.Code != http.StatusOK {
		t.Fatalf("re-register deleted username status = %d, body = %s", reReg.Code, reReg.Body.String())
	}

	// Delete non-existent -> 404
	notFound := adminReq(http.MethodDelete, "/admin/users/9999", "")
	if notFound.Code != http.StatusNotFound {
		t.Fatalf("delete missing user status = %d, want 404", notFound.Code)
	}
}

func TestAdminDeleteUserCascadesAccounts(t *testing.T) {
	app, handler := newTestApp(t)
	currentAppRef = app
	defer func() { currentAppRef = nil }()

	_, id := registerUser(t, handler, "cascadeUser", "secret123")
	nick := "owned-account"
	status := "alive"
	if _, err := app.db.UpsertAccount(context.Background(), "openid-cascade", "buf", &nick, &nick, nil, nil, nil, &status, &id); err != nil {
		t.Fatalf("upsert account: %v", err)
	}
	if _, err := app.db.GetAccountByOpenID(context.Background(), "openid-cascade"); err != nil {
		t.Fatalf("account should exist before delete: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/admin/users/"+strconv.FormatInt(id, 10), nil)
	req.Header.Set("Authorization", "Bearer "+currentAPIToken())
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d", rec.Code)
	}

	if _, err := app.db.GetAccountByOpenID(context.Background(), "openid-cascade"); err == nil {
		t.Fatalf("account should be cascaded after user delete")
	}
}
