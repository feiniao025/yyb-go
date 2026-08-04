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

type sessionEntry struct {
	user      string
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

func requireAuth(w http.ResponseWriter, r *http.Request) bool {
	// Paths that don't need auth
	if r.URL.Path == "/login" || r.URL.Path == "/health" || r.URL.Path == "/token" || r.URL.Path == "/account/logout" || r.URL.Path == "/openapi.json" || r.URL.Path == "/features" || strings.HasPrefix(r.URL.Path, "/docs") {
		return true
	}

	// Check Bearer token for API calls
	auth := r.Header.Get("Authorization")
	if len(auth) > 7 && auth[:7] == "Bearer " && auth[7:] == currentAPIToken() {
		return true
	}

	// Fall back to session cookie (for browser)
	c, err := r.Cookie(cookieName)
	if err == nil {
		sessionsMu.Lock()
		s, ok := sessions[c.Value]
		sessionsMu.Unlock()
		if ok && !time.Now().After(s.expiresAt) {
			return true
		}
	}

	// API calls without token -> 401
	if len(r.URL.Path) >= 6 && (r.URL.Path[:6] == "/wxapp" || r.URL.Path[:9] == "/accounts" || r.URL.Path[:4] == "/qr" || r.URL.Path[:4] == "/docs") {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"code":401,"msg":"invalid api token","data":null}`))
		return false
	}

	// Browser -> redirect to login
	http.Redirect(w, r, "/login", http.StatusFound)
	return false
}

func authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requireAuth(w, r) {
			next.ServeHTTP(w, r)
		}
	})
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
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

	if u != adminUser || !verifyPassword(p, adminPass) {
		http.Redirect(w, r, "/login?err=1", http.StatusFound)
		return
	}

	sid := generateSessionID()
	sessionsMu.Lock()
	sessions[sid] = sessionEntry{user: u, expiresAt: time.Now().Add(24 * time.Hour)}
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

func invalidateAllSessions() {
	sessionsMu.Lock()
	for k := range sessions {
		delete(sessions, k)
	}
	sessionsMu.Unlock()
}

// handlePasswordChange verifies the old password and persists a new one.
func handlePasswordChange(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
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

// handleLogout removes the current session and clears the cookie.
func handleLogout(w http.ResponseWriter, r *http.Request) {
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
