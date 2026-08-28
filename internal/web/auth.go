package web

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

var (
	sessMu   sync.RWMutex
	sessions = map[string]time.Time{}
)

const sessionTTL = 12 * time.Hour

func newToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method", 405)
		return
	}
	var req struct{ User, Pass string }
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", 400)
		return
	}
	uOK := subtle.ConstantTimeCompare([]byte(req.User), []byte(s.cfg.AdminUser)) == 1
	pOK := subtle.ConstantTimeCompare([]byte(req.Pass), []byte(s.cfg.AdminPass)) == 1
	if !uOK || !pOK {
		http.Error(w, "用户名或密码错误", 401)
		return
	}
	tok := newToken()
	sessMu.Lock()
	sessions[tok] = time.Now().Add(sessionTTL)
	sessMu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "session", Value: tok, Path: "/", HttpOnly: true, MaxAge: int(sessionTTL.Seconds()), SameSite: http.SameSiteLaxMode})
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("session")
		if err != nil {
			http.Error(w, "未登录", 401)
			return
		}
		sessMu.RLock()
		exp, ok := sessions[c.Value]
		sessMu.RUnlock()
		if !ok || time.Now().After(exp) {
			http.Error(w, "会话过期", 401)
			return
		}
		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}
