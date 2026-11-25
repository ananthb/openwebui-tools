// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestFNSClientMintAnd401Refresh(t *testing.T) {
	var mintCount atomic.Int32
	var apiCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/internal/token":
			if r.Header.Get("Authorization") != "Bearer fns-secret" {
				http.Error(w, "bad mint secret", http.StatusUnauthorized)
				return
			}
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["email"] != "alice@example.com" || body["client"] != "openwebui-tools" ||
				body["scope"] != "p:rest c:openwebui-tools f:*" || body["expiredDays"] != float64(7) {
				http.Error(w, "unexpected mint body", http.StatusBadRequest)
				return
			}
			n := mintCount.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]string{"token": "fns-tok-" + string(rune('0'+n))})
		case "/api/vault":
			if r.Header.Get("X-Client") != "openwebui-tools" {
				http.Error(w, "missing X-Client", http.StatusBadRequest)
				return
			}
			n := apiCount.Add(1)
			if n == 1 {
				http.Error(w, "token revoked", http.StatusUnauthorized)
				return
			}
			if r.Header.Get("Authorization") != "Bearer fns-tok-2" {
				http.Error(w, "expected refreshed token", http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []string{"MainVault"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	s := newServer(config{
		StorePath:     filepath.Join(t.TempDir(), "tokens.json"),
		FNSURL:        srv.URL,
		FNSMintSecret: "fns-secret",
	})
	ctx := ctxWithUser("u1", "alice@example.com", "Alice")

	res, _, err := s.fnsVaults(ctx, nil, Empty{})
	if err != nil {
		t.Fatalf("fnsVaults: %v", err)
	}
	if !strings.Contains(resultText(t, res), "MainVault") {
		t.Fatalf("unexpected fnsVaults result: %s", resultText(t, res))
	}
	if mintCount.Load() != 2 {
		t.Fatalf("mintCount = %d, want 2", mintCount.Load())
	}

	// Subsequent call should reuse cached token without minting again
	if _, _, err := s.fnsVaults(ctx, nil, Empty{}); err != nil {
		t.Fatalf("second fnsVaults: %v", err)
	}
	if mintCount.Load() != 2 {
		t.Fatalf("mintCount after cached call = %d, want 2", mintCount.Load())
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
		if r.URL.Path == "/api/internal/token" {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{"token": "tok-1"}})
			return
		}
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
		StorePath:     filepath.Join(t.TempDir(), "tokens.json"),
		FNSURL:        srv.URL,
		FNSMintSecret: "sec",
	})
	ctx := ctxWithUser("u1", "alice@example.com", "Alice")

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
