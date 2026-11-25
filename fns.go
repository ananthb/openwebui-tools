// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const fnsClientName = "openwebui-tools"

type FNSClient struct {
	baseURL    string
	mintSecret string

	mu         sync.Mutex
	userTokens map[string]string // email -> token
}

func newFNSClient(baseURL, mintSecret string) *FNSClient {
	return &FNSClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		mintSecret: mintSecret,
		userTokens: map[string]string{},
	}
}

func (c *FNSClient) userToken(ctx context.Context, email string) (string, error) {
	if c.baseURL == "" {
		return "", fmt.Errorf("FNS_URL is not configured")
	}
	if c.mintSecret == "" {
		return "", fmt.Errorf("FNS_MINT_SECRET is not configured")
	}

	c.mu.Lock()
	if t, ok := c.userTokens[email]; ok {
		c.mu.Unlock()
		return t, nil
	}
	c.mu.Unlock()

	payload := map[string]any{
		"email":       email,
		"client":      fnsClientName,
		"scope":       "p:rest c:" + fnsClientName + " f:*",
		"expiredDays": 7,
	}
	code, b, err := doJSON(ctx, http.MethodPost, c.baseURL+"/api/internal/token",
		map[string]string{"Authorization": "Bearer " + c.mintSecret}, payload)
	if err != nil {
		return "", err
	}
	if code >= 400 {
		return "", fmt.Errorf("fns mint http %d: %s", code, snippet(b))
	}

	var out struct {
		Token string `json:"token"`
		Data  struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return "", fmt.Errorf("fns mint: bad response %s", snippet(b))
	}
	tok := out.Token
	if tok == "" {
		tok = out.Data.Token
	}
	if tok == "" {
		return "", fmt.Errorf("fns mint: empty token in response %s", snippet(b))
	}

	c.mu.Lock()
	c.userTokens[email] = tok
	c.mu.Unlock()
	return tok, nil
}

func (c *FNSClient) invalidateUserToken(email, token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cur, ok := c.userTokens[email]; ok && cur == token {
		delete(c.userTokens, email)
	}
}

func (c *FNSClient) doUser(ctx context.Context, email, method, apiPath string, payload any) ([]byte, error) {
	for attempt := 0; attempt < 2; attempt++ {
		tok, err := c.userToken(ctx, email)
		if err != nil {
			return nil, err
		}
		headers := map[string]string{
			"Authorization": "Bearer " + tok,
			"X-Client":      fnsClientName,
		}
		code, b, err := doJSON(ctx, method, c.baseURL+apiPath, headers, payload)
		if err != nil {
			return nil, err
		}
		if code == http.StatusUnauthorized && attempt == 0 {
			c.invalidateUserToken(email, tok)
			continue
		}
		if code >= 400 {
			return nil, fmt.Errorf("fns http %d: %s", code, snippet(b))
		}
		return b, nil
	}
	return nil, fmt.Errorf("fns request failed after token refresh")
}

// --- Tool input structs ------------------------------------------------------

type fnsFolderTreeIn struct {
	Vault string `json:"vault" jsonschema:"vault name"`
	Depth int    `json:"depth,omitempty" jsonschema:"optional tree depth"`
}

type fnsFolderIn struct {
	Vault string `json:"vault" jsonschema:"vault name"`
	Path  string `json:"path" jsonschema:"folder path"`
}

type fnsNotesListIn struct {
	Vault         string `json:"vault" jsonschema:"vault name"`
	Keyword       string `json:"keyword,omitempty" jsonschema:"optional search keyword"`
	SearchMode    string `json:"search_mode,omitempty" jsonschema:"'path' or 'content'"`
	SearchContent bool   `json:"search_content,omitempty" jsonschema:"search note content"`
	IsRecycle     bool   `json:"is_recycle,omitempty" jsonschema:"list notes in recycle bin"`
	SortBy        string `json:"sort_by,omitempty" jsonschema:"sort field e.g. mtime"`
	SortOrder     string `json:"sort_order,omitempty" jsonschema:"asc or desc"`
	Page          int    `json:"page,omitempty" jsonschema:"page number"`
	PageSize      int    `json:"page_size,omitempty" jsonschema:"page size"`
}

type fnsNoteGetIn struct {
	Vault     string `json:"vault" jsonschema:"vault name"`
	Path      string `json:"path" jsonschema:"note path"`
	IsRecycle bool   `json:"is_recycle,omitempty" jsonschema:"fetch from recycle bin"`
}

type fnsNoteSaveIn struct {
	Vault      string `json:"vault" jsonschema:"vault name"`
	Path       string `json:"path" jsonschema:"note path"`
	Content    string `json:"content" jsonschema:"full markdown note content"`
	CreateOnly bool   `json:"create_only,omitempty" jsonschema:"if true, fail if note already exists"`
}

type fnsNoteContentIn struct {
	Vault   string `json:"vault" jsonschema:"vault name"`
	Path    string `json:"path" jsonschema:"note path"`
	Content string `json:"content" jsonschema:"content to append or prepend"`
}

type fnsNoteReplaceIn struct {
	Vault         string `json:"vault" jsonschema:"vault name"`
	Path          string `json:"path" jsonschema:"note path"`
	Find          string `json:"find" jsonschema:"text or regex pattern to find"`
	Replace       string `json:"replace" jsonschema:"replacement text"`
	Regex         bool   `json:"regex,omitempty" jsonschema:"treat find as regex"`
	All           bool   `json:"all,omitempty" jsonschema:"replace all matches"`
	FailIfNoMatch bool   `json:"fail_if_no_match,omitempty" jsonschema:"fail if no match is found"`
}

type fnsNoteFrontmatterIn struct {
	Vault   string         `json:"vault" jsonschema:"vault name"`
	Path    string         `json:"path" jsonschema:"note path"`
	Updates map[string]any `json:"updates,omitempty" jsonschema:"frontmatter fields to set/update"`
	Remove  []string       `json:"remove,omitempty" jsonschema:"frontmatter field names to remove"`
}

type fnsNoteRenameIn struct {
	Vault   string `json:"vault" jsonschema:"vault name"`
	OldPath string `json:"old_path" jsonschema:"current note path"`
	Path    string `json:"path" jsonschema:"new note path"`
}

type fnsNotePathIn struct {
	Vault string `json:"vault" jsonschema:"vault name"`
	Path  string `json:"path" jsonschema:"note path"`
}

func (s *server) registerFNSTools(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{Name: "fns_vaults", Description: "List your Obsidian Fast Note Sync vaults."}, s.fnsVaults)
	mcp.AddTool(srv, &mcp.Tool{Name: "fns_folder_tree", Description: "Get the folder tree of an Obsidian FNS vault."}, s.fnsFolderTree)
	mcp.AddTool(srv, &mcp.Tool{Name: "fns_folder_create", Description: "Create a folder in an Obsidian FNS vault."}, s.fnsFolderCreate)
	mcp.AddTool(srv, &mcp.Tool{Name: "fns_folder_delete", Description: "Delete a folder in an Obsidian FNS vault."}, s.fnsFolderDelete)
	mcp.AddTool(srv, &mcp.Tool{Name: "fns_notes_list", Description: "List or search notes in an Obsidian FNS vault."}, s.fnsNotesList)
	mcp.AddTool(srv, &mcp.Tool{Name: "fns_note_get", Description: "Read a note from an Obsidian FNS vault."}, s.fnsNoteGet)
	mcp.AddTool(srv, &mcp.Tool{Name: "fns_note_save", Description: "Create or overwrite a note in an Obsidian FNS vault."}, s.fnsNoteSave)
	mcp.AddTool(srv, &mcp.Tool{Name: "fns_note_append", Description: "Append content to the end of an Obsidian FNS note."}, s.fnsNoteAppend)
	mcp.AddTool(srv, &mcp.Tool{Name: "fns_note_prepend", Description: "Prepend content to an Obsidian FNS note (after frontmatter)."}, s.fnsNotePrepend)
	mcp.AddTool(srv, &mcp.Tool{Name: "fns_note_replace", Description: "Find and replace text in an Obsidian FNS note."}, s.fnsNoteReplace)
	mcp.AddTool(srv, &mcp.Tool{Name: "fns_note_frontmatter", Description: "Patch frontmatter fields on an Obsidian FNS note."}, s.fnsNoteFrontmatter)
	mcp.AddTool(srv, &mcp.Tool{Name: "fns_note_rename", Description: "Rename or move an Obsidian FNS note."}, s.fnsNoteRename)
	mcp.AddTool(srv, &mcp.Tool{Name: "fns_note_delete", Description: "Move an Obsidian FNS note to trash."}, s.fnsNoteDelete)
	mcp.AddTool(srv, &mcp.Tool{Name: "fns_note_restore", Description: "Restore an Obsidian FNS note from trash."}, s.fnsNoteRestore)
	mcp.AddTool(srv, &mcp.Tool{Name: "fns_note_links", Description: "Get backlinks and outlinks for an Obsidian FNS note."}, s.fnsNoteLinks)
}

func (s *server) fnsCall(ctx context.Context, method, apiPath string, payload any) (*mcp.CallToolResult, any, error) {
	email, err := requireEmail(ctx)
	if err != nil {
		return fail(err)
	}
	b, err := s.fns.doUser(ctx, email, method, apiPath, payload)
	if err != nil {
		return fail(err)
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return text("ok")
	}
	if json.Valid(b) {
		return text(json.RawMessage(b))
	}
	return text(string(b))
}

func (s *server) fnsVaults(ctx context.Context, _ *mcp.CallToolRequest, _ Empty) (*mcp.CallToolResult, any, error) {
	return s.fnsCall(ctx, http.MethodGet, "/api/vault", nil)
}

func (s *server) fnsFolderTree(ctx context.Context, _ *mcp.CallToolRequest, in fnsFolderTreeIn) (*mcp.CallToolResult, any, error) {
	q := url.Values{"vault": {in.Vault}}
	if in.Depth > 0 {
		q.Set("depth", strconv.Itoa(in.Depth))
	}
	return s.fnsCall(ctx, http.MethodGet, "/api/folder/tree?"+q.Encode(), nil)
}

func (s *server) fnsFolderCreate(ctx context.Context, _ *mcp.CallToolRequest, in fnsFolderIn) (*mcp.CallToolResult, any, error) {
	return s.fnsCall(ctx, http.MethodPost, "/api/folder", map[string]any{
		"vault": in.Vault,
		"path":  in.Path,
	})
}

func (s *server) fnsFolderDelete(ctx context.Context, _ *mcp.CallToolRequest, in fnsFolderIn) (*mcp.CallToolResult, any, error) {
	return s.fnsCall(ctx, http.MethodDelete, "/api/folder", map[string]any{
		"vault": in.Vault,
		"path":  in.Path,
	})
}

func (s *server) fnsNotesList(ctx context.Context, _ *mcp.CallToolRequest, in fnsNotesListIn) (*mcp.CallToolResult, any, error) {
	q := url.Values{"vault": {in.Vault}}
	if in.Keyword != "" {
		q.Set("keyword", in.Keyword)
	}
	if in.SearchMode != "" {
		q.Set("searchMode", in.SearchMode)
	}
	if in.SearchContent {
		q.Set("searchContent", "true")
	}
	if in.IsRecycle {
		q.Set("isRecycle", "true")
	}
	if in.SortBy != "" {
		q.Set("sortBy", in.SortBy)
	}
	if in.SortOrder != "" {
		q.Set("sortOrder", in.SortOrder)
	}
	if in.Page > 0 {
		q.Set("page", strconv.Itoa(in.Page))
	}
	if in.PageSize > 0 {
		q.Set("pageSize", strconv.Itoa(in.PageSize))
	}
	return s.fnsCall(ctx, http.MethodGet, "/api/notes?"+q.Encode(), nil)
}

func (s *server) fnsNoteGet(ctx context.Context, _ *mcp.CallToolRequest, in fnsNoteGetIn) (*mcp.CallToolResult, any, error) {
	q := url.Values{"vault": {in.Vault}, "path": {in.Path}}
	if in.IsRecycle {
		q.Set("isRecycle", "true")
	}
	return s.fnsCall(ctx, http.MethodGet, "/api/note?"+q.Encode(), nil)
}

func (s *server) fnsNoteSave(ctx context.Context, _ *mcp.CallToolRequest, in fnsNoteSaveIn) (*mcp.CallToolResult, any, error) {
	payload := map[string]any{
		"vault":   in.Vault,
		"path":    in.Path,
		"content": in.Content,
	}
	if in.CreateOnly {
		payload["createOnly"] = true
	}
	return s.fnsCall(ctx, http.MethodPost, "/api/note", payload)
}

func (s *server) fnsNoteAppend(ctx context.Context, _ *mcp.CallToolRequest, in fnsNoteContentIn) (*mcp.CallToolResult, any, error) {
	return s.fnsCall(ctx, http.MethodPost, "/api/note/append", map[string]any{
		"vault":   in.Vault,
		"path":    in.Path,
		"content": in.Content,
	})
}

func (s *server) fnsNotePrepend(ctx context.Context, _ *mcp.CallToolRequest, in fnsNoteContentIn) (*mcp.CallToolResult, any, error) {
	return s.fnsCall(ctx, http.MethodPost, "/api/note/prepend", map[string]any{
		"vault":   in.Vault,
		"path":    in.Path,
		"content": in.Content,
	})
}

func (s *server) fnsNoteReplace(ctx context.Context, _ *mcp.CallToolRequest, in fnsNoteReplaceIn) (*mcp.CallToolResult, any, error) {
	return s.fnsCall(ctx, http.MethodPost, "/api/note/replace", map[string]any{
		"vault":         in.Vault,
		"path":          in.Path,
		"find":          in.Find,
		"replace":       in.Replace,
		"regex":         in.Regex,
		"all":           in.All,
		"failIfNoMatch": in.FailIfNoMatch,
	})
}

func (s *server) fnsNoteFrontmatter(ctx context.Context, _ *mcp.CallToolRequest, in fnsNoteFrontmatterIn) (*mcp.CallToolResult, any, error) {
	payload := map[string]any{
		"vault": in.Vault,
		"path":  in.Path,
	}
	if len(in.Updates) > 0 {
		payload["updates"] = in.Updates
	}
	if len(in.Remove) > 0 {
		payload["remove"] = in.Remove
	}
	return s.fnsCall(ctx, http.MethodPatch, "/api/note/frontmatter", payload)
}

func (s *server) fnsNoteRename(ctx context.Context, _ *mcp.CallToolRequest, in fnsNoteRenameIn) (*mcp.CallToolResult, any, error) {
	return s.fnsCall(ctx, http.MethodPost, "/api/note/rename", map[string]any{
		"vault":   in.Vault,
		"oldPath": in.OldPath,
		"path":    in.Path,
	})
}

func (s *server) fnsNoteDelete(ctx context.Context, _ *mcp.CallToolRequest, in fnsNotePathIn) (*mcp.CallToolResult, any, error) {
	q := url.Values{"vault": {in.Vault}, "path": {in.Path}}
	return s.fnsCall(ctx, http.MethodDelete, "/api/note?"+q.Encode(), nil)
}

func (s *server) fnsNoteRestore(ctx context.Context, _ *mcp.CallToolRequest, in fnsNotePathIn) (*mcp.CallToolResult, any, error) {
	return s.fnsCall(ctx, http.MethodPut, "/api/note/restore", map[string]any{
		"vault": in.Vault,
		"path":  in.Path,
	})
}

func (s *server) fnsNoteLinks(ctx context.Context, _ *mcp.CallToolRequest, in fnsNotePathIn) (*mcp.CallToolResult, any, error) {
	email, err := requireEmail(ctx)
	if err != nil {
		return fail(err)
	}
	q := url.Values{"vault": {in.Vault}, "path": {in.Path}}.Encode()
	back, err := s.fns.doUser(ctx, email, http.MethodGet, "/api/note/backlinks?"+q, nil)
	if err != nil {
		return fail(err)
	}
	out, err := s.fns.doUser(ctx, email, http.MethodGet, "/api/note/outlinks?"+q, nil)
	if err != nil {
		return fail(err)
	}
	return text(map[string]any{
		"backlinks": json.RawMessage(back),
		"outlinks":  json.RawMessage(out),
	})
}
