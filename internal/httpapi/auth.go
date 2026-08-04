package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"yyb_go/internal/store"
)

const cookieName = "yyb_go_session"

// 通过环境变量配置，空值使用默认值
func envOr(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

func randomToken() string {
	b := make([]byte, 32)
	_, _ = io.ReadFull(rand.Reader, b)
	return hexEncode(b)
}

const passwordSettingKey = "admin_password"

// hashPassword returns "salt$hash" using sha256 with a random 32-byte salt.
func hashPassword(pw string) string {
	salt := make([]byte, 32)
	_, _ = io.ReadFull(rand.Reader, salt)
	sum := sha256.Sum256(append(salt, []byte(pw)...))
	return hex.EncodeToString(salt) + "$" + hex.EncodeToString(sum[:])
}

// verifyPassword checks pw against a stored value which is either
// "salt$hash" (hashed) or a plaintext fallback (legacy/default).
func verifyPassword(pw, stored string) bool {
	if stored == "" {
		return false
	}
	if salt, hash, ok := strings.Cut(stored, "$"); ok {
		saltBytes, err := hex.DecodeString(salt)
		if err != nil {
			return false
		}
		sum := sha256.Sum256(append(saltBytes, []byte(pw)...))
		return subtle.ConstantTimeCompare([]byte(hex.EncodeToString(sum[:])), []byte(hash)) == 1
	}
	return subtle.ConstantTimeCompare([]byte(pw), []byte(stored)) == 1
}

// loadStoredPassword overrides adminPass with the persisted hash when the
// password is not configured via environment variable.
func loadStoredPassword(ctx context.Context, db *store.DB) {
	if adminPassFromEnv {
		return
	}
	v, ok, err := db.GetSetting(ctx, passwordSettingKey)
	if err == nil && ok && v != "" {
		adminPass = v
	}
}

var (
	adminUser        = envOr("YYB_ADMIN_USER", "admin")
	adminPass        = envOr("YYB_ADMIN_PASS", "admin")
	adminPassFromEnv = os.Getenv("YYB_ADMIN_PASS") != ""
	apiTokenFromEnv  = os.Getenv("YYB_API_TOKEN")
	apiToken         = func() string {
		if apiTokenFromEnv != "" {
			return apiTokenFromEnv
		}
		return randomToken()
	}()
	apiTokenMu sync.Mutex
)

func currentAPIToken() string {
	apiTokenMu.Lock()
	defer apiTokenMu.Unlock()
	return apiToken
}

func rotateAPIToken() (string, bool) {
	apiTokenMu.Lock()
	defer apiTokenMu.Unlock()
	if apiTokenFromEnv != "" {
		return apiToken, false
	}
	apiToken = randomToken()
	return apiToken, true
}

// identity describes the authenticated principal of a request.
type identity struct {
	isAdmin  bool
	userID   int64
	username string
}

func adminIdentity() identity {
	return identity{isAdmin: true, username: adminUser}
}

func (id identity) ownerID() *int64 {
	if id.isAdmin {
		return nil
	}
	v := id.userID
	return &v
}

type sessionEntry struct {
	user      string
	identity  identity
	expiresAt time.Time
}

var (
	sessions   = map[string]sessionEntry{}
	sessionsMu sync.Mutex
)

func sessionCleanupLoop() {
	for range time.NewTicker(5 * time.Minute).C {
		sessionsMu.Lock()
		now := time.Now()
		for k, s := range sessions {
			if now.After(s.expiresAt) {
				delete(sessions, k)
			}
		}
		sessionsMu.Unlock()
	}
}

func hexEncode(b []byte) string {
	const hex = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2] = hex[v>>4]
		out[i*2+1] = hex[v&0x0f]
	}
	return string(out)
}

func generateSessionID() string {
	b := make([]byte, 32)
	_, _ = io.ReadFull(rand.Reader, b)
	return hexEncode(b)
}

func (a *App) requireAuth(w http.ResponseWriter, r *http.Request) bool {
	// Paths that don't need auth
	if r.URL.Path == "/login" || r.URL.Path == "/register" || r.URL.Path == "/health" || r.URL.Path == "/openapi.json" || r.URL.Path == "/features" || strings.HasPrefix(r.URL.Path, "/docs") {
		return true
	}

	if _, ok := a.currentIdentity(w, r); ok {
		return true
	}

	// API calls without token -> 401
	if isAPIPath(r.URL.Path) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"code":401,"msg":"invalid api token","data":null}`))
		return false
	}

	// Browser -> redirect to login
	http.Redirect(w, r, "/login", http.StatusFound)
	return false
}

// isAPIPath reports whether the path is a programmatic API endpoint that must
// respond with 401 JSON instead of a browser redirect when unauthenticated.
func isAPIPath(path string) bool {
	for _, prefix := range []string{"/wxapp", "/accounts", "/qr", "/token", "/account", "/admin"} {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}

// currentIdentity resolves the authenticated principal from a Bearer token or
// session cookie. It returns ok=false when no valid credential is present or
// the associated user has been disabled.
func (a *App) currentIdentity(w http.ResponseWriter, r *http.Request) (identity, bool) {
	// Check Bearer token for API calls
	auth := r.Header.Get("Authorization")
	if len(auth) > 7 && auth[:7] == "Bearer " {
		token := auth[7:]
		if token == currentAPIToken() {
			return adminIdentity(), true
		}
		if u, err := a.db.GetUserByToken(r.Context(), token); err == nil && u != nil {
			if u.Disabled {
				return identity{}, false
			}
			return identity{userID: u.ID, username: u.Username}, true
		}
		return identity{}, false
	}

	// Fall back to session cookie (for browser)
	c, err := r.Cookie(cookieName)
	if err == nil {
		sessionsMu.Lock()
		s, ok := sessions[c.Value]
		sessionsMu.Unlock()
		if ok && !time.Now().After(s.expiresAt) {
			return s.identity, true
		}
	}

	return identity{}, false
}

func (a *App) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.requireAuth(w, r) {
			next.ServeHTTP(w, r)
		}
	})
}

func (a *App) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		errFlag := "none"
		if r.URL.Query().Get("err") == "1" {
			errFlag = "block"
		}
		w.Write([]byte(`<!doctype html><html lang="zh-CN"><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>YYB Go - 登录</title>
<style>
*{box-sizing:border-box}html,body{height:100%;margin:0}
body{display:grid;place-items:center;background:radial-gradient(900px 560px at 12% -10%,rgba(59,130,246,.18),transparent 60%),radial-gradient(760px 480px at 108% 8%,rgba(167,139,250,.15),transparent 58%),#0b1220;color:#e6edf7;font-family:ui-sans-serif,system-ui,-apple-system,BlinkMacSystemFont,'Segoe UI','Microsoft YaHei',sans-serif;font-size:14px;-webkit-font-smoothing:antialiased}
.card{width:min(380px,calc(100vw-32px));background:rgba(17,24,39,.72);border:1px solid rgba(148,163,184,.16);border-radius:10px;padding:32px;box-shadow:0 12px 32px rgba(2,6,23,.6),inset 0 1px 0 rgba(255,255,255,.06);backdrop-filter:blur(12px);-webkit-backdrop-filter:blur(12px)}
h1{margin:0 0 4px;font-size:20px;text-align:center;letter-spacing:-.01em}
.sub{margin:0 0 24px;text-align:center;color:#94a3b8;font-size:13px}
label{display:block;margin-bottom:4px;font-size:13px;color:#94a3b8;font-weight:500}
input{width:100%;padding:9px 11px;border:1px solid rgba(148,163,184,.3);border-radius:8px;margin-bottom:16px;outline:none;background:rgba(255,255,255,.04);color:#e6edf7;font:inherit;transition:border-color 160ms ease,box-shadow 160ms ease}
input:focus{border-color:#3b82f6;box-shadow:0 0 0 3px rgba(59,130,246,.25)}
button{width:100%;padding:10px;border:0;border-radius:8px;background:linear-gradient(135deg,#3b82f6,#6366f1);color:#fff;font-size:14px;font-weight:600;cursor:pointer;box-shadow:0 4px 14px rgba(59,130,246,.35);transition:filter 160ms ease}
button:hover{filter:brightness(1.08)}
button:active{transform:translateY(1px)}
.err{color:#f87171;font-size:13px;margin:12px 0 0;text-align:center;display:` + errFlag + `}
.register{margin:16px 0 0;text-align:center;font-size:13px;color:#94a3b8}
.register a{color:#60a5fa;text-decoration:none}
.register a:hover{text-decoration:underline}
</style>
<div class="card">
<h1>YYB Go</h1>
<p class="sub">微信扫码登录控制台</p>
<form method="post" action="/login">
<label for="u">用户名</label>
<input id="u" name="username" required autocomplete="username" autofocus>
<label for="p">密码</label>
<input id="p" type="password" name="password" required autocomplete="current-password">
<button type="submit">登录</button>
<p class="err">用户名或密码错误</p>
</form>
<p class="register">没有账号？<a href="/register">注册新账号</a></p>
</div>
</body></html>`))
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}

	r.ParseForm()
	u := r.FormValue("username")
	p := r.FormValue("password")

	id, ok := a.verifyCredentials(r, u, p)
	if !ok {
		http.Redirect(w, r, "/login?err=1", http.StatusFound)
		return
	}

	sid := generateSessionID()
	sessionsMu.Lock()
	sessions[sid] = sessionEntry{user: u, identity: id, expiresAt: time.Now().Add(24 * time.Hour)}
	sessionsMu.Unlock()

	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    sid,
		Path:     "/",
		HttpOnly: true,
		MaxAge:   86400,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/", http.StatusFound)
}

// verifyCredentials validates a username/password pair and returns the
// matching identity. Admin credentials are checked first, then registered users.
// Disabled users cannot log in.
func (a *App) verifyCredentials(r *http.Request, username, password string) (identity, bool) {
	if username == adminUser && verifyPassword(password, adminPass) {
		return adminIdentity(), true
	}
	u, err := a.db.GetUserByUsername(r.Context(), username)
	if err != nil || u == nil {
		return identity{}, false
	}
	if u.Disabled {
		return identity{}, false
	}
	if !verifyPassword(password, u.Password) {
		return identity{}, false
	}
	return identity{userID: u.ID, username: u.Username}, true
}

// requireAdmin asserts the request is authenticated as admin.
func (a *App) requireAdmin(w http.ResponseWriter, r *http.Request) (identity, bool) {
	id, ok := a.currentIdentity(w, r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "invalid api token")
		return identity{}, false
	}
	if !id.isAdmin {
		writeError(w, http.StatusForbidden, "admin only")
		return identity{}, false
	}
	return id, true
}

func invalidateAllSessions() {
	sessionsMu.Lock()
	for k := range sessions {
		delete(sessions, k)
	}
	sessionsMu.Unlock()
}

// handlePasswordChange verifies the old password and persists a new one.
// Admin password changes follow the existing global mechanism; registered
// users change their own per-user password.
func (a *App) handlePasswordChange(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	id, ok := a.currentIdentity(w, r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "invalid api token")
		return
	}
	if id.isAdmin {
		a.handleAdminPasswordChange(w, r)
		return
	}
	var body struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := decodeOptionalJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if body.OldPassword == "" {
		writeError(w, http.StatusBadRequest, "旧密码不能为空")
		return
	}
	if body.NewPassword == "" {
		writeError(w, http.StatusBadRequest, "新密码不能为空")
		return
	}
	if body.NewPassword == body.OldPassword {
		writeError(w, http.StatusBadRequest, "新密码不能与旧密码相同")
		return
	}
	u, err := a.db.GetUser(r.Context(), id.userID)
	if err != nil || u == nil {
		writeError(w, http.StatusUnauthorized, "user not found")
		return
	}
	if !verifyPassword(body.OldPassword, u.Password) {
		writeError(w, http.StatusBadRequest, "旧密码不正确")
		return
	}
	if len(body.NewPassword) < 6 {
		writeError(w, http.StatusBadRequest, "新密码至少需要 6 位")
		return
	}
	if err := a.db.UpdateUserPassword(r.Context(), id.userID, hashPassword(body.NewPassword)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.invalidateUserSessions(id.userID)
	writeJSON(w, http.StatusOK, map[string]any{"changed": true})
}

func (a *App) handleAdminPasswordChange(w http.ResponseWriter, r *http.Request) {
	if adminPassFromEnv {
		writeError(w, http.StatusForbidden, "密码由环境变量 YYB_ADMIN_PASS 配置，不支持在页面修改")
		return
	}
	var body struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := decodeOptionalJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if body.OldPassword == "" {
		writeError(w, http.StatusBadRequest, "旧密码不能为空")
		return
	}
	if !verifyPassword(body.OldPassword, adminPass) {
		writeError(w, http.StatusBadRequest, "旧密码不正确")
		return
	}
	if body.NewPassword == "" {
		writeError(w, http.StatusBadRequest, "新密码不能为空")
		return
	}
	if body.NewPassword == body.OldPassword {
		writeError(w, http.StatusBadRequest, "新密码不能与旧密码相同")
		return
	}
	adminPass = hashPassword(body.NewPassword)
	invalidateAllSessions()
	writeJSON(w, http.StatusOK, map[string]any{"changed": true})
}

func (a *App) invalidateUserSessions(userID int64) {
	sessionsMu.Lock()
	for k, s := range sessions {
		if !s.identity.isAdmin && s.identity.userID == userID {
			delete(sessions, k)
		}
	}
	sessionsMu.Unlock()
}

// handleLogout removes the current session and clears the cookie.
func (a *App) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if c, err := r.Cookie(cookieName); err == nil {
		sessionsMu.Lock()
		delete(sessions, c.Value)
		sessionsMu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
	writeJSON(w, http.StatusOK, map[string]any{"logged_out": true})
}

// handleRegister serves the registration page (GET) and creates a new user
// (POST). On success it returns the user's isolated API token.
func (a *App) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		errFlag := "none"
		if r.URL.Query().Get("err") == "1" {
			errFlag = "block"
		}
		w.Write([]byte(`<!doctype html><html lang="zh-CN"><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>YYB Go - 注册</title>
<style>
*{box-sizing:border-box}html,body{height:100%;margin:0}
body{display:grid;place-items:center;background:radial-gradient(900px 560px at 12% -10%,rgba(59,130,246,.18),transparent 60%),radial-gradient(760px 480px at 108% 8%,rgba(167,139,250,.15),transparent 58%),#0b1220;color:#e6edf7;font-family:ui-sans-serif,system-ui,-apple-system,BlinkMacSystemFont,'Segoe UI','Microsoft YaHei',sans-serif;font-size:14px;-webkit-font-smoothing:antialiased}
.card{width:min(380px,calc(100vw-32px));background:rgba(17,24,39,.72);border:1px solid rgba(148,163,184,.16);border-radius:10px;padding:32px;box-shadow:0 12px 32px rgba(2,6,23,.6),inset 0 1px 0 rgba(255,255,255,.06);backdrop-filter:blur(12px);-webkit-backdrop-filter:blur(12px)}
h1{margin:0 0 4px;font-size:20px;text-align:center;letter-spacing:-.01em}
.sub{margin:0 0 24px;text-align:center;color:#94a3b8;font-size:13px}
label{display:block;margin-bottom:4px;font-size:13px;color:#94a3b8;font-weight:500}
input{width:100%;padding:9px 11px;border:1px solid rgba(148,163,184,.3);border-radius:8px;margin-bottom:16px;outline:none;background:rgba(255,255,255,.04);color:#e6edf7;font:inherit;transition:border-color 160ms ease,box-shadow 160ms ease}
input:focus{border-color:#3b82f6;box-shadow:0 0 0 3px rgba(59,130,246,.25)}
button{width:100%;padding:10px;border:0;border-radius:8px;background:linear-gradient(135deg,#3b82f6,#6366f1);color:#fff;font-size:14px;font-weight:600;cursor:pointer;box-shadow:0 4px 14px rgba(59,130,246,.35);transition:filter 160ms ease}
button:hover{filter:brightness(1.08)}
button:active{transform:translateY(1px)}
.err{color:#f87171;font-size:13px;margin:12px 0 0;text-align:center;display:` + errFlag + `}
.register{margin:16px 0 0;text-align:center;font-size:13px;color:#94a3b8}
.register a{color:#60a5fa;text-decoration:none}
.register a:hover{text-decoration:underline}
.hint{font-size:12px;color:#94a3b8;margin:-10px 0 16px}
</style>
<div class="card">
<h1>注册账号</h1>
<p class="sub">注册后将获得独立 API Token，数据相互隔离</p>
<form method="post" action="/register">
<label for="u">用户名</label>
<input id="u" name="username" required autocomplete="username" autofocus>
<label for="p">密码</label>
<input id="p" type="password" name="password" required minlength="6" autocomplete="new-password">
<p class="hint">密码至少 6 位</p>
<button type="submit">注册</button>
<p class="err">用户名已存在或参数不合法</p>
</form>
<p class="register">已有账号？<a href="/login">返回登录</a></p>
</div>
</body></html>`))
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}

	ct := r.Header.Get("Content-Type")
	if strings.Contains(ct, "application/json") {
		a.handleRegisterAPI(w, r)
		return
	}

	r.ParseForm()
	u := strings.TrimSpace(r.FormValue("username"))
	p := r.FormValue("password")
	confirm := r.FormValue("confirm")

	if u == "" || p == "" {
		http.Redirect(w, r, "/register?err=1", http.StatusFound)
		return
	}
	if u == adminUser {
		http.Redirect(w, r, "/register?err=1", http.StatusFound)
		return
	}
	if len(p) < 6 {
		http.Redirect(w, r, "/register?err=1", http.StatusFound)
		return
	}
	if confirm != "" && confirm != p {
		http.Redirect(w, r, "/register?err=1", http.StatusFound)
		return
	}

	token := randomToken()
	if _, err := a.db.CreateUser(r.Context(), u, hashPassword(p), token); err != nil {
		http.Redirect(w, r, "/register?err=1", http.StatusFound)
		return
	}

	http.Redirect(w, r, "/login", http.StatusFound)
}

// handleRegisterAPI creates a user via a JSON API and returns the isolated
// token. Used by programmatic clients.
func (a *App) handleRegisterAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeOptionalJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	body.Username = strings.TrimSpace(body.Username)
	if body.Username == "" || body.Password == "" {
		writeError(w, http.StatusBadRequest, "username and password are required")
		return
	}
	if body.Username == adminUser {
		writeError(w, http.StatusConflict, "username already exists")
		return
	}
	if len(body.Password) < 6 {
		writeError(w, http.StatusBadRequest, "password must be at least 6 characters")
		return
	}
	if _, err := a.db.GetUserByUsername(r.Context(), body.Username); err == nil {
		writeError(w, http.StatusConflict, "username already exists")
		return
	}
	token := randomToken()
	u, err := a.db.CreateUser(r.Context(), body.Username, hashPassword(body.Password), token)
	if err != nil {
		writeError(w, http.StatusConflict, "username already exists")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"username": u.Username, "api_token": u.APIToken})
}
