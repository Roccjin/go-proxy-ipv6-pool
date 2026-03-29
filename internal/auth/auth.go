package auth

import (
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strings"
	"sync"
)

type Manager struct {
	mu    sync.RWMutex
	users map[string]string // username -> password
}

func New() *Manager {
	return &Manager{users: make(map[string]string)}
}

func (m *Manager) AddUser(user, pass string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.users[user] = pass
}

func (m *Manager) RemoveUser(user string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.users, user)
}

func (m *Manager) Validate(user, pass string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	expected, ok := m.users[user]
	if !ok {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(pass)) == 1
}

func (m *Manager) ListUsers() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	users := make([]string, 0, len(m.users))
	for u := range m.users {
		users = append(users, u)
	}
	return users
}

// ParseProxyAuth extracts user:pass from Proxy-Authorization header
func ParseProxyAuth(r *http.Request) (string, string, bool) {
	auth := r.Header.Get("Proxy-Authorization")
	if auth == "" {
		return "", "", false
	}
	if !strings.HasPrefix(auth, "Basic ") {
		return "", "", false
	}
	decoded, err := base64.StdEncoding.DecodeString(auth[6:])
	if err != nil {
		return "", "", false
	}
	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}
