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

func TestSeafileTokenRefreshOn401(t *testing.T) {
	var mintCount atomic.Int32
	var apiCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2.1/admin/generate-user-auth-token/":
			if r.Header.Get("Authorization") != "Token adm-tok" {
				http.Error(w, "bad admin", http.StatusUnauthorized)
				return
			}
			n := mintCount.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]string{"token": "user-tok-" + string(rune('0'+n))})
		case "/api/v2.1/repos/":
			n := apiCount.Add(1)
			if n == 1 {
				http.Error(w, "expired", http.StatusUnauthorized)
				return
			}
			if r.Header.Get("Authorization") != "Token user-tok-2" {
				http.Error(w, "expected refreshed token", http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"repos": []map[string]any{{"repo_id": "r1", "repo_name": "Notes", "size": 42}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	s := newServer(config{
		StorePath:         filepath.Join(t.TempDir(), "tokens.json"),
		SeafileBase:       srv.URL,
		SeafileAdminToken: "adm-tok",
	})
	ctx := ctxWithUser("u1", "alice@example.com", "Alice")

	res, _, err := s.seafileLibraries(ctx, nil, Empty{})
	if err != nil {
		t.Fatalf("seafileLibraries: %v", err)
	}
	got := resultText(t, res)
	if !strings.Contains(got, `"name":"Notes"`) {
		t.Fatalf("unexpected result: %s", got)
	}
	if mintCount.Load() != 2 {
		t.Fatalf("mintCount = %d, want 2", mintCount.Load())
	}
	if apiCount.Load() != 2 {
		t.Fatalf("apiCount = %d, want 2", apiCount.Load())
	}
}

func TestSeafileWriteAndOperations(t *testing.T) {
	type reqRecord struct {
		Method string
		Path   string
		Query  string
		Body   string
	}
	var recorded []reqRecord

	var srvURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2.1/admin/generate-user-auth-token/":
			_ = json.NewEncoder(w).Encode(map[string]string{"token": "sf-tok"})
		case "/api/v2.1/repos/":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"repos": []map[string]any{
					{"repo_id": "repo-1", "repo_name": "Docs"},
					{"repo_id": "repo-2", "repo_name": "Archive"},
				},
			})
		case "/api/v2.1/search/":
			recorded = append(recorded, reqRecord{r.Method, r.URL.Path, r.URL.RawQuery, ""})
			_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{}})
		case "/api2/repos/repo-1/dir/":
			b, _ := io.ReadAll(r.Body)
			recorded = append(recorded, reqRecord{r.Method, r.URL.Path, r.URL.RawQuery, string(b)})
			_, _ = w.Write([]byte(`"success"`))
		case "/api2/repos/repo-1/upload-link/":
			recorded = append(recorded, reqRecord{r.Method, r.URL.Path, r.URL.RawQuery, ""})
			_ = json.NewEncoder(w).Encode(srvURL + "/seafhttp/upload-api/abc")
		case "/seafhttp/upload-api/abc":
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			f, hdr, err := r.FormFile("file")
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			fc, _ := io.ReadAll(f)
			f.Close()
			recorded = append(recorded, reqRecord{
				r.Method,
				r.URL.Path,
				"parent_dir=" + r.FormValue("parent_dir") + "&replace=" + r.FormValue("replace") + "&filename=" + hdr.Filename,
				string(fc),
			})
			_, _ = w.Write([]byte(`[{"id":"file1"}]`))
		case "/api2/repos/repo-1/file/":
			b, _ := io.ReadAll(r.Body)
			recorded = append(recorded, reqRecord{r.Method, r.URL.Path, r.URL.RawQuery, string(b)})
			_, _ = w.Write([]byte(`"success"`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	srvURL = srv.URL

	s := newServer(config{
		StorePath:         filepath.Join(t.TempDir(), "tokens.json"),
		SeafileBase:       srv.URL,
		SeafileAdminToken: "adm",
	})
	ctx := ctxWithUser("u1", "alice@example.com", "Alice")

	if _, _, err := s.seafileSearch(ctx, nil, sfSearchIn{Query: "budget"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.seafileMkdir(ctx, nil, sfPathIn{Path: "Docs/2026"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.seafileWrite(ctx, nil, sfWriteIn{Path: "Docs/2026/note.txt", Content: "hello world"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.seafileRenameMove(ctx, nil, sfRenameMoveIn{
		Path:      "Docs/2026/note.txt",
		Operation: "move",
		DstPath:   "Archive/old",
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.seafileDelete(ctx, nil, sfDeleteIn{Path: "Docs/2026/note.txt"}); err != nil {
		t.Fatal(err)
	}

	if len(recorded) != 6 {
		t.Fatalf("recorded %d calls, want 6: %+v", len(recorded), recorded)
	}
	if recorded[0].Path != "/api/v2.1/search/" || recorded[0].Query != "q=budget" {
		t.Fatalf("search call = %+v", recorded[0])
	}
	if recorded[1].Method != http.MethodPost || recorded[1].Body != "operation=mkdir" {
		t.Fatalf("mkdir call = %+v", recorded[1])
	}
	if recorded[3].Path != "/seafhttp/upload-api/abc" || recorded[3].Body != "hello world" ||
		!strings.Contains(recorded[3].Query, "parent_dir=/2026") ||
		!strings.Contains(recorded[3].Query, "replace=1") ||
		!strings.Contains(recorded[3].Query, "filename=note.txt") {
		t.Fatalf("upload call = %+v", recorded[3])
	}
	if recorded[4].Method != http.MethodPost || !strings.Contains(recorded[4].Body, "operation=move") ||
		!strings.Contains(recorded[4].Body, "dst_repo=repo-2") || !strings.Contains(recorded[4].Body, "dst_dir=%2Fold") {
		t.Fatalf("move call = %+v", recorded[4])
	}
	if recorded[5].Method != http.MethodDelete || recorded[5].Path != "/api2/repos/repo-1/file/" {
		t.Fatalf("delete call = %+v", recorded[5])
	}
}
