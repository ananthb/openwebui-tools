// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestFNSClientUserTokenAnd401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/vault" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("X-Client") != "openwebui-tools" {
			http.Error(w, "missing X-Client", http.StatusBadRequest)
			return
		}
		if r.Header.Get("Authorization") != "Bearer fns-tok-good" {
			http.Error(w, "token revoked", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []string{"MainVault"}})
	}))
	defer srv.Close()

	s := newServer(config{
		StorePath: filepath.Join(t.TempDir(), "tokens.json"),
		FNSURL:    srv.URL,
	})
	ctx := ctxWithUser("u1", "alice@example.com", "Alice")

	// Missing token should prompt set_token(app="obsidian", token=...)
	res, _, err := s.fnsVaults(ctx, nil, Empty{})
	if err != nil {
		t.Fatalf("fnsVaults without token: %v", err)
	}
	if msg := resultText(t, res); !strings.Contains(msg, `set_token(app="obsidian", token=...)`) {
		t.Fatalf("expected set_token prompt, got: %s", msg)
	}

	// Revoked/expired token should return 401 message prompting re-setting token
	if _, _, err := s.setToken(ctx, nil, tokenIn{App: "fns", Token: "fns-tok-old"}); err != nil {
		t.Fatalf("setToken (fns alias): %v", err)
	}
	res, _, err = s.fnsVaults(ctx, nil, Empty{})
	if err != nil {
		t.Fatalf("fnsVaults 401: %v", err)
	}
	if msg := resultText(t, res); !strings.Contains(msg, "401") || !strings.Contains(msg, `set_token(app="obsidian", token=...)`) {
		t.Fatalf("expected 401 re-set prompt, got: %s", msg)
	}

	// Valid token succeeds
	if _, _, err := s.setToken(ctx, nil, tokenIn{App: "obsidian", Token: "fns-tok-good"}); err != nil {
		t.Fatalf("setToken (obsidian): %v", err)
	}
	res, _, err = s.fnsVaults(ctx, nil, Empty{})
	if err != nil {
		t.Fatalf("fnsVaults valid: %v", err)
	}
	if !strings.Contains(resultText(t, res), "MainVault") {
		t.Fatalf("unexpected fnsVaults result: %s", resultText(t, res))
	}
}

func TestFNSToolsFormatting(t *testing.T) {
	type call struct {
		Method string
		Path   string
		Query  string
		Body   string
	}
	var calls []call

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok-1" || r.Header.Get("X-Client") != "openwebui-tools" {
			http.Error(w, "missing auth headers", http.StatusUnauthorized)
			return
		}
		b, _ := io.ReadAll(r.Body)
		calls = append(calls, call{r.Method, r.URL.Path, r.URL.RawQuery, string(b)})
		_, _ = w.Write([]byte(`{"code":1,"data":{}}`))
	}))
	defer srv.Close()

	s := newServer(config{
		StorePath: filepath.Join(t.TempDir(), "tokens.json"),
		FNSURL:    srv.URL,
	})
	ctx := ctxWithUser("u1", "alice@example.com", "Alice")
	if _, _, err := s.setToken(ctx, nil, tokenIn{App: "obsidian", Token: "tok-1"}); err != nil {
		t.Fatalf("setToken: %v", err)
	}

	_, _, _ = s.fnsFolderTree(ctx, nil, fnsFolderTreeIn{Vault: "V", Depth: 2})
	_, _, _ = s.fnsFolderCreate(ctx, nil, fnsFolderIn{Vault: "V", Path: "Projects"})
	_, _, _ = s.fnsFolderDelete(ctx, nil, fnsFolderIn{Vault: "V", Path: "Projects"})
	_, _, _ = s.fnsNotesList(ctx, nil, fnsNotesListIn{Vault: "V", Keyword: "todo", SearchMode: "content"})
	_, _, _ = s.fnsNoteGet(ctx, nil, fnsNoteGetIn{Vault: "V", Path: "a.md"})
	_, _, _ = s.fnsNoteSave(ctx, nil, fnsNoteSaveIn{Vault: "V", Path: "a.md", Content: "# Hi"})
	_, _, _ = s.fnsNoteAppend(ctx, nil, fnsNoteContentIn{Vault: "V", Path: "a.md", Content: "more"})
	_, _, _ = s.fnsNotePrepend(ctx, nil, fnsNoteContentIn{Vault: "V", Path: "a.md", Content: "top"})
	_, _, _ = s.fnsNoteReplace(ctx, nil, fnsNoteReplaceIn{Vault: "V", Path: "a.md", Find: "old", Replace: "new", All: true})
	_, _, _ = s.fnsNoteFrontmatter(ctx, nil, fnsNoteFrontmatterIn{Vault: "V", Path: "a.md", Updates: map[string]any{"tag": "work"}})
	_, _, _ = s.fnsNoteRename(ctx, nil, fnsNoteRenameIn{Vault: "V", OldPath: "a.md", Path: "b.md"})
	_, _, _ = s.fnsNoteDelete(ctx, nil, fnsNotePathIn{Vault: "V", Path: "b.md"})
	_, _, _ = s.fnsNoteRestore(ctx, nil, fnsNotePathIn{Vault: "V", Path: "b.md"})
	_, _, _ = s.fnsNoteLinks(ctx, nil, fnsNotePathIn{Vault: "V", Path: "b.md"})

	want := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/folder/tree"},
		{http.MethodPost, "/api/folder"},
		{http.MethodDelete, "/api/folder"},
		{http.MethodGet, "/api/notes"},
		{http.MethodGet, "/api/note"},
		{http.MethodPost, "/api/note"},
		{http.MethodPost, "/api/note/append"},
		{http.MethodPost, "/api/note/prepend"},
		{http.MethodPost, "/api/note/replace"},
		{http.MethodPatch, "/api/note/frontmatter"},
		{http.MethodPost, "/api/note/rename"},
		{http.MethodDelete, "/api/note"},
		{http.MethodPut, "/api/note/restore"},
		{http.MethodGet, "/api/note/backlinks"},
		{http.MethodGet, "/api/note/outlinks"},
	}
	if len(calls) != len(want) {
		t.Fatalf("got %d calls, want %d", len(calls), len(want))
	}
	for i, w := range want {
		if calls[i].Method != w.method || calls[i].Path != w.path {
			t.Fatalf("call[%d] = %s %s, want %s %s", i, calls[i].Method, calls[i].Path, w.method, w.path)
		}
	}
}
