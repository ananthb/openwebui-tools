// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var httpClient = &http.Client{Timeout: 120 * time.Second}

// Empty is the input for tools that take no arguments.
type Empty struct{}

type tokenIn struct {
	App   string `json:"app" jsonschema:"app name: mealie, immich, or obsidian (alias: fns)"`
	Token string `json:"token" jsonschema:"your API token; empty to forget"`
}

// do issues a request, backing off on 429 (Seafile throttles per user).
func do(ctx context.Context, method, rawurl string, headers map[string]string, body []byte) (int, []byte, error) {
	for attempt := 0; ; attempt++ {
		var r io.Reader
		if body != nil {
			r = bytes.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, rawurl, r)
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

func doJSON(ctx context.Context, method, rawurl string, headers map[string]string, payload any) (int, []byte, error) {
	var body []byte
	h := headers
	if payload != nil {
		var err error
		body, err = json.Marshal(payload)
		if err != nil {
			return 0, nil, err
		}
		h = withJSON(headers)
	}
	return do(ctx, method, rawurl, h, body)
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 300 {
		return s[:300]
	}
	return s
}

func withJSON(h map[string]string) map[string]string {
	out := map[string]string{"Content-Type": "application/json"}
	for k, v := range h {
		out[k] = v
	}
	return out
}

func text(v any) (*mcp.CallToolResult, any, error) {
	var s string
	if str, ok := v.(string); ok {
		s = str
	} else {
		b, err := json.Marshal(v)
		if err != nil {
			return nil, nil, err
		}
		s = string(b)
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}, nil, nil
}

func fail(err error) (*mcp.CallToolResult, any, error) {
	return text(fmt.Sprintf("error: %v", err))
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

// appToken returns the caller's token for an app, falling back to the shared one.
func (s *server) appToken(user *User, app, shared string) string {
	if t, ok := s.store.get(user.ID, app); ok {
		return t
	}
	return shared
}

func (s *server) registerAuthTools(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "set_token",
		Description: "Store your personal token for an app (mealie, immich, or obsidian; fns is accepted as an alias for obsidian). For obsidian, mint a REST token in the Fast Note Sync WebGUI with scope p:rest c:* f:* or c:openwebui-tools. Call with an empty token to forget it.",
	}, s.setToken)
	mcp.AddTool(srv, &mcp.Tool{Name: "token_status", Description: "Show the logged-in user and which apps have tokens or auto-auth configured."}, s.tokenStatus)
}

func (s *server) setToken(ctx context.Context, _ *mcp.CallToolRequest, in tokenIn) (*mcp.CallToolResult, any, error) {
	u := userFrom(ctx)
	if u == nil {
		return nil, nil, fmt.Errorf("unauthenticated")
	}
	app := strings.ToLower(strings.TrimSpace(in.App))
	if app == "fns" {
		app = "obsidian"
	}
	if app != "mealie" && app != "immich" && app != "obsidian" {
		return fail(fmt.Errorf("app must be mealie, immich, or obsidian"))
	}
	tok := strings.TrimSpace(in.Token)
	if err := s.store.set(u.ID, app, tok); err != nil {
		return fail(err)
	}
	if tok == "" {
		return text("forgotten")
	}
	return text("stored")
}

func (s *server) tokenStatus(ctx context.Context, _ *mcp.CallToolRequest, _ Empty) (*mcp.CallToolResult, any, error) {
	u := userFrom(ctx)
	if u == nil {
		return nil, nil, fmt.Errorf("unauthenticated")
	}
	_, hasMealie := s.store.get(u.ID, "mealie")
	_, hasImmich := s.store.get(u.ID, "immich")
	_, hasObsidian := s.store.get(u.ID, "obsidian")

	seafileAuto := s.sf.baseURL != "" && (s.sf.cfgAdminToken != "" || (s.sf.adminEmail != "" && s.sf.adminPassword != ""))

	return text(map[string]any{
		"user": map[string]string{
			"id":    u.ID,
			"email": u.Email,
			"name":  u.Name,
		},
		"apps": map[string]any{
			"seafile": map[string]any{
				"configured": seafileAuto,
				"auth":       "auto",
			},
			"obsidian": map[string]any{
				"configured":     s.fns.baseURL != "",
				"personal_token": hasObsidian,
			},
			"mealie": map[string]any{
				"configured":      s.cfg.MealieURL != "",
				"personal_token":  hasMealie,
				"shared_fallback": s.cfg.MealieToken != "",
			},
			"immich": map[string]any{
				"configured":      s.cfg.ImmichURL != "",
				"personal_token":  hasImmich,
				"shared_fallback": s.cfg.ImmichKey != "",
			},
		},
	})
}
