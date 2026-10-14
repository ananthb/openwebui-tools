// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var httpClient = &http.Client{Timeout: 120 * time.Second}

// do issues a request, backing off on 429 (Seafile throttles per user).
func do(ctx context.Context, method, rawurl string, headers map[string]string, body []byte) (int, []byte, error) {
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, rawurl, bytes.NewReader(body))
		if err != nil {
			return 0, nil, err
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			return 0, nil, err
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests && attempt < 4 {
			time.Sleep(time.Duration(2*(attempt+1)) * time.Second)
			continue
		}
		return resp.StatusCode, b, nil
	}
}

func getJSON(ctx context.Context, rawurl string, headers map[string]string) (any, error) {
	code, b, err := do(ctx, http.MethodGet, rawurl, headers, nil)
	if err != nil {
		return nil, err
	}
	if code >= 400 {
		return nil, fmt.Errorf("http %d: %s", code, snippet(b))
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 300 {
		return s[:300]
	}
	return s
}

// --- per-user token store (file-backed; shared tokens come from config) ------

type tokenStore struct {
	mu   sync.Mutex
	path string
	data map[string]map[string]string
}

func newTokenStore(path string) *tokenStore {
	s := &tokenStore{path: path, data: map[string]map[string]string{}}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &s.data)
	}
	return s
}

func (s *tokenStore) get(user, app string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.data[user][app]
	return t, ok && t != ""
}

func (s *tokenStore) set(user, app, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data[user] == nil {
		s.data[user] = map[string]string{}
	}
	if token == "" {
		delete(s.data[user], app)
	} else {
		s.data[user][app] = token
	}
	return s.save()
}

func (s *tokenStore) save() error {
	b, err := json.Marshal(s.data)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// --- Seafile -----------------------------------------------------------------

type seafile struct {
	adminMu    sync.Mutex
	adminToken string
	userMu     sync.Mutex
	userTokens map[string]string // email -> token
}

func (s *server) seafileAdmin(ctx context.Context) (string, error) {
	s.sf.adminMu.Lock()
	defer s.sf.adminMu.Unlock()
	if s.sf.adminToken != "" {
		return s.sf.adminToken, nil
	}
	if s.cfg.SeafileAdminToken != "" {
		s.sf.adminToken = s.cfg.SeafileAdminToken
		return s.sf.adminToken, nil
	}
	form := url.Values{"username": {s.cfg.SeafileAdminEmail}, "password": {s.cfg.SeafileAdminPassword}}
	code, b, err := do(ctx, http.MethodPost, s.cfg.SeafileBase+"/api2/auth-token/",
		map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, []byte(form.Encode()))
	if err != nil {
		return "", err
	}
	if code >= 400 {
		return "", fmt.Errorf("seafile admin login http %d: %s", code, snippet(b))
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(b, &out); err != nil || out.Token == "" {
		return "", fmt.Errorf("seafile admin login: bad response %s", snippet(b))
	}
	s.sf.adminToken = out.Token
	return out.Token, nil
}

func (s *server) seafileUser(ctx context.Context, email string) (string, error) {
	s.sf.userMu.Lock()
	if t, ok := s.sf.userTokens[email]; ok {
		s.sf.userMu.Unlock()
		return t, nil
	}
	s.sf.userMu.Unlock()
	admin, err := s.seafileAdmin(ctx)
	if err != nil {
		return "", err
	}
	form := url.Values{"email": {email}}
	code, b, err := do(ctx, http.MethodPost, s.cfg.SeafileBase+"/api/v2.1/admin/generate-user-auth-token/",
		map[string]string{"Authorization": "Token " + admin, "Content-Type": "application/x-www-form-urlencoded"}, []byte(form.Encode()))
	if err != nil {
		return "", err
	}
	if code >= 400 {
		return "", fmt.Errorf("seafile mint http %d: %s", code, snippet(b))
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(b, &out); err != nil || out.Token == "" {
		return "", fmt.Errorf("seafile mint: bad response %s", snippet(b))
	}
	s.sf.userMu.Lock()
	s.sf.userTokens[email] = out.Token
	s.sf.userMu.Unlock()
	return out.Token, nil
}

func (s *server) seafileRepos(ctx context.Context, token string) ([]map[string]any, error) {
	v, err := getJSON(ctx, s.cfg.SeafileBase+"/api/v2.1/repos/?per_page=100", map[string]string{"Authorization": "Token " + token})
	if err != nil {
		return nil, err
	}
	repos, _ := v.(map[string]any)["repos"].([]any)
	out := make([]map[string]any, 0, len(repos))
	for _, r := range repos {
		if m, ok := r.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out, nil
}

func (s *server) seafileResolve(ctx context.Context, token, path string) (string, string, error) {
	parts := strings.FieldsFunc(path, func(r rune) bool { return r == '/' })
	if len(parts) == 0 {
		return "", "", fmt.Errorf("empty path")
	}
	repos, err := s.seafileRepos(ctx, token)
	if err != nil {
		return "", "", err
	}
	for _, r := range repos {
		if r["repo_name"] == parts[0] {
			return r["repo_id"].(string), "/" + strings.Join(parts[1:], "/"), nil
		}
	}
	return "", "", fmt.Errorf("no library named %q", parts[0])
}
