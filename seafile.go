// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type SeafileClient struct {
	baseURL       string
	adminEmail    string
	adminPassword string
	cfgAdminToken string

	adminMu    sync.Mutex
	adminToken string
	userMu     sync.Mutex
	userTokens map[string]string // email -> token
}

func newSeafileClient(baseURL, adminEmail, adminPassword, adminToken string) *SeafileClient {
	return &SeafileClient{
		baseURL:       strings.TrimRight(baseURL, "/"),
		adminEmail:    adminEmail,
		adminPassword: adminPassword,
		cfgAdminToken: adminToken,
		userTokens:    map[string]string{},
	}
}

func (c *SeafileClient) admin(ctx context.Context) (string, error) {
	c.adminMu.Lock()
	defer c.adminMu.Unlock()
	if c.adminToken != "" {
		return c.adminToken, nil
	}
	if c.cfgAdminToken != "" {
		c.adminToken = c.cfgAdminToken
		return c.adminToken, nil
	}
	form := url.Values{"username": {c.adminEmail}, "password": {c.adminPassword}}
	code, b, err := do(ctx, http.MethodPost, c.baseURL+"/api2/auth-token/",
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
	c.adminToken = out.Token
	return out.Token, nil
}

func (c *SeafileClient) userToken(ctx context.Context, email string) (string, error) {
	c.userMu.Lock()
	if t, ok := c.userTokens[email]; ok {
		c.userMu.Unlock()
		return t, nil
	}
	c.userMu.Unlock()

	admin, err := c.admin(ctx)
	if err != nil {
		return "", err
	}
	form := url.Values{"email": {email}}
	code, b, err := do(ctx, http.MethodPost, c.baseURL+"/api/v2.1/admin/generate-user-auth-token/",
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
	c.userMu.Lock()
	c.userTokens[email] = out.Token
	c.userMu.Unlock()
	return out.Token, nil
}

func (c *SeafileClient) invalidateUserToken(email, token string) {
	c.userMu.Lock()
	defer c.userMu.Unlock()
	if cur, ok := c.userTokens[email]; ok && cur == token {
		delete(c.userTokens, email)
	}
}

// doUser performs a Seafile API request for email, refreshing the cached user
// token once if Seafile responds with 401 Unauthorized.
func (c *SeafileClient) doUser(ctx context.Context, email, method, apiPath string, headers map[string]string, body []byte) (int, []byte, error) {
	for attempt := 0; attempt < 2; attempt++ {
		token, err := c.userToken(ctx, email)
		if err != nil {
			return 0, nil, err
		}
		h := make(map[string]string, len(headers)+1)
		for k, v := range headers {
			h[k] = v
		}
		h["Authorization"] = "Token " + token

		code, b, err := do(ctx, method, c.baseURL+apiPath, h, body)
		if err != nil {
			return 0, nil, err
		}
		if code == http.StatusUnauthorized && attempt == 0 {
			c.invalidateUserToken(email, token)
			continue
		}
		return code, b, nil
	}
	return 0, nil, fmt.Errorf("seafile request failed after token refresh")
}

func (c *SeafileClient) getJSONUser(ctx context.Context, email, apiPath string) (any, error) {
	code, b, err := c.doUser(ctx, email, http.MethodGet, apiPath, nil, nil)
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

func (c *SeafileClient) repos(ctx context.Context, email string) ([]map[string]any, error) {
	v, err := c.getJSONUser(ctx, email, "/api/v2.1/repos/?per_page=100")
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

func (c *SeafileClient) resolve(ctx context.Context, email, p string) (string, string, error) {
	parts := strings.FieldsFunc(p, func(r rune) bool { return r == '/' })
	if len(parts) == 0 {
		return "", "", fmt.Errorf("empty path")
	}
	repos, err := c.repos(ctx, email)
	if err != nil {
		return "", "", err
	}
	for _, r := range repos {
		if r["repo_name"] == parts[0] {
			repoID, _ := r["repo_id"].(string)
			return repoID, "/" + strings.Join(parts[1:], "/"), nil
		}
	}
	return "", "", fmt.Errorf("no library named %q", parts[0])
}

func (c *SeafileClient) rewriteURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", err
	}
	target := c.baseURL + u.Path
	if u.RawQuery != "" {
		target += "?" + u.RawQuery
	}
	return target, nil
}

// --- Tool input structs ------------------------------------------------------

type sfPathIn struct {
	Path string `json:"path" jsonschema:"Seafile path: 'Library' or 'Library/sub/file.txt'"`
}

type sfSearchIn struct {
	Query string `json:"query" jsonschema:"search query"`
}

type sfWriteIn struct {
	Path    string `json:"path" jsonschema:"Seafile file path: 'Library/sub/file.txt'"`
	Content string `json:"content" jsonschema:"UTF-8 file content to write (overwrites if exists)"`
}

type sfDeleteIn struct {
	Path  string `json:"path" jsonschema:"Seafile path to delete: 'Library/sub/file.txt' or 'Library/sub/dir'"`
	IsDir bool   `json:"is_dir,omitempty" jsonschema:"true to delete a directory instead of a file"`
}

type sfRenameMoveIn struct {
	Path      string `json:"path" jsonschema:"source Seafile path: 'Library/sub/item'"`
	Operation string `json:"operation" jsonschema:"'rename' or 'move'"`
	NewName   string `json:"newname,omitempty" jsonschema:"new name (required when operation is 'rename')"`
	DstPath   string `json:"dst_path,omitempty" jsonschema:"destination folder path e.g. 'Library/other/dir' (required when operation is 'move')"`
	IsDir     bool   `json:"is_dir,omitempty" jsonschema:"true if source is a directory"`
}

func (s *server) registerSeafileTools(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{Name: "seafile_libraries", Description: "List the Seafile libraries you can access."}, s.seafileLibraries)
	mcp.AddTool(srv, &mcp.Tool{Name: "seafile_browse", Description: "List one Seafile folder. path is 'Library' or 'Library/sub/dir'."}, s.seafileBrowse)
	mcp.AddTool(srv, &mcp.Tool{Name: "seafile_read", Description: "Read a text file from Seafile. path is 'Library/sub/file.txt'."}, s.seafileRead)
	mcp.AddTool(srv, &mcp.Tool{Name: "seafile_search", Description: "Search files and folders across your Seafile libraries."}, s.seafileSearch)
	mcp.AddTool(srv, &mcp.Tool{Name: "seafile_mkdir", Description: "Create a directory in Seafile. path is 'Library/sub/newdir'."}, s.seafileMkdir)
	mcp.AddTool(srv, &mcp.Tool{Name: "seafile_write", Description: "Upload or overwrite a text file in Seafile. path is 'Library/sub/file.txt'."}, s.seafileWrite)
	mcp.AddTool(srv, &mcp.Tool{Name: "seafile_delete", Description: "Delete a file or directory in Seafile."}, s.seafileDelete)
	mcp.AddTool(srv, &mcp.Tool{Name: "seafile_rename_move", Description: "Rename or move a file or directory in Seafile."}, s.seafileRenameMove)
}

func requireEmail(ctx context.Context) (string, error) {
	u := userFrom(ctx)
	if u == nil {
		return "", fmt.Errorf("unauthenticated")
	}
	if u.Email == "" {
		return "", fmt.Errorf("user has no email in JWT")
	}
	return u.Email, nil
}

func (s *server) seafileLibraries(ctx context.Context, _ *mcp.CallToolRequest, _ Empty) (*mcp.CallToolResult, any, error) {
	email, err := requireEmail(ctx)
	if err != nil {
		return fail(err)
	}
	repos, err := s.sf.repos(ctx, email)
	if err != nil {
		return fail(err)
	}
	out := make([]map[string]any, 0, len(repos))
	for _, r := range repos {
		out = append(out, map[string]any{"name": r["repo_name"], "size": r["size"]})
	}
	return text(out)
}

func (s *server) seafileBrowse(ctx context.Context, _ *mcp.CallToolRequest, in sfPathIn) (*mcp.CallToolResult, any, error) {
	email, err := requireEmail(ctx)
	if err != nil {
		return fail(err)
	}
	repo, sub, err := s.sf.resolve(ctx, email, in.Path)
	if err != nil {
		return fail(err)
	}
	v, err := s.sf.getJSONUser(ctx, email, "/api2/repos/"+repo+"/dir/?"+url.Values{"p": {sub}}.Encode())
	if err != nil {
		return fail(err)
	}
	return text(v)
}

func (s *server) seafileRead(ctx context.Context, _ *mcp.CallToolRequest, in sfPathIn) (*mcp.CallToolResult, any, error) {
	email, err := requireEmail(ctx)
	if err != nil {
		return fail(err)
	}
	repo, sub, err := s.sf.resolve(ctx, email, in.Path)
	if err != nil {
		return fail(err)
	}
	code, b, err := s.sf.doUser(ctx, email, http.MethodGet, "/api2/repos/"+repo+"/file/?"+url.Values{"p": {sub}}.Encode(), nil, nil)
	if err != nil {
		return fail(err)
	}
	if code >= 400 {
		return fail(fmt.Errorf("seafile http %d: %s", code, snippet(b)))
	}
	var link string
	if err := json.Unmarshal(b, &link); err != nil {
		return text(string(b))
	}
	if !isText(in.Path) {
		return text("Not a text file; download it at " + link)
	}
	target, err := s.sf.rewriteURL(link)
	if err != nil {
		return fail(err)
	}
	code, body, err := do(ctx, http.MethodGet, target, nil, nil)
	if err != nil {
		return fail(err)
	}
	if code >= 400 {
		return fail(fmt.Errorf("seafile download http %d", code))
	}
	if len(body) > 200000 {
		body = body[:200000]
	}
	return text(string(body))
}

func (s *server) seafileSearch(ctx context.Context, _ *mcp.CallToolRequest, in sfSearchIn) (*mcp.CallToolResult, any, error) {
	email, err := requireEmail(ctx)
	if err != nil {
		return fail(err)
	}
	v, err := s.sf.getJSONUser(ctx, email, "/api/v2.1/search/?"+url.Values{"q": {in.Query}}.Encode())
	if err != nil {
		return fail(err)
	}
	return text(v)
}

func (s *server) seafileMkdir(ctx context.Context, _ *mcp.CallToolRequest, in sfPathIn) (*mcp.CallToolResult, any, error) {
	email, err := requireEmail(ctx)
	if err != nil {
		return fail(err)
	}
	repo, sub, err := s.sf.resolve(ctx, email, in.Path)
	if err != nil {
		return fail(err)
	}
	form := url.Values{"operation": {"mkdir"}}
	code, b, err := s.sf.doUser(ctx, email, http.MethodPost,
		"/api2/repos/"+repo+"/dir/?"+url.Values{"p": {sub}}.Encode(),
		map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
		[]byte(form.Encode()))
	if err != nil {
		return fail(err)
	}
	if code >= 400 {
		return fail(fmt.Errorf("seafile mkdir http %d: %s", code, snippet(b)))
	}
	return text(string(b))
}

func (s *server) seafileWrite(ctx context.Context, _ *mcp.CallToolRequest, in sfWriteIn) (*mcp.CallToolResult, any, error) {
	email, err := requireEmail(ctx)
	if err != nil {
		return fail(err)
	}
	repo, sub, err := s.sf.resolve(ctx, email, in.Path)
	if err != nil {
		return fail(err)
	}
	if sub == "/" {
		return fail(fmt.Errorf("path must include a filename"))
	}
	parentDir := path.Dir(sub)
	if parentDir == "." || parentDir == "" {
		parentDir = "/"
	}
	filename := path.Base(sub)

	code, b, err := s.sf.doUser(ctx, email, http.MethodGet,
		"/api2/repos/"+repo+"/upload-link/?"+url.Values{"p": {parentDir}}.Encode(), nil, nil)
	if err != nil {
		return fail(err)
	}
	if code >= 400 {
		return fail(fmt.Errorf("seafile upload-link http %d: %s", code, snippet(b)))
	}
	var link string
	if err := json.Unmarshal(b, &link); err != nil {
		link = strings.Trim(strings.TrimSpace(string(b)), `"`)
	}
	target, err := s.sf.rewriteURL(link)
	if err != nil {
		return fail(err)
	}

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("parent_dir", parentDir)
	_ = mw.WriteField("replace", "1")
	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		return fail(err)
	}
	if _, err := fw.Write([]byte(in.Content)); err != nil {
		return fail(err)
	}
	if err := mw.Close(); err != nil {
		return fail(err)
	}

	token, err := s.sf.userToken(ctx, email)
	if err != nil {
		return fail(err)
	}
	code, respBody, err := do(ctx, http.MethodPost, target, map[string]string{
		"Authorization": "Token " + token,
		"Content-Type":  mw.FormDataContentType(),
	}, buf.Bytes())
	if err != nil {
		return fail(err)
	}
	if code >= 400 {
		return fail(fmt.Errorf("seafile upload http %d: %s", code, snippet(respBody)))
	}
	return text(string(respBody))
}

func (s *server) seafileDelete(ctx context.Context, _ *mcp.CallToolRequest, in sfDeleteIn) (*mcp.CallToolResult, any, error) {
	email, err := requireEmail(ctx)
	if err != nil {
		return fail(err)
	}
	repo, sub, err := s.sf.resolve(ctx, email, in.Path)
	if err != nil {
		return fail(err)
	}
	kind := "file"
	if in.IsDir {
		kind = "dir"
	}
	code, b, err := s.sf.doUser(ctx, email, http.MethodDelete,
		"/api2/repos/"+repo+"/"+kind+"/?"+url.Values{"p": {sub}}.Encode(), nil, nil)
	if err != nil {
		return fail(err)
	}
	if code >= 400 {
		return fail(fmt.Errorf("seafile delete http %d: %s", code, snippet(b)))
	}
	return text(string(b))
}

func (s *server) seafileRenameMove(ctx context.Context, _ *mcp.CallToolRequest, in sfRenameMoveIn) (*mcp.CallToolResult, any, error) {
	email, err := requireEmail(ctx)
	if err != nil {
		return fail(err)
	}
	if in.Operation != "rename" && in.Operation != "move" {
		return fail(fmt.Errorf("operation must be 'rename' or 'move'"))
	}
	repo, sub, err := s.sf.resolve(ctx, email, in.Path)
	if err != nil {
		return fail(err)
	}
	form := url.Values{"operation": {in.Operation}}
	if in.Operation == "rename" {
		if in.NewName == "" {
			return fail(fmt.Errorf("newname is required for rename"))
		}
		form.Set("newname", in.NewName)
	} else {
		if in.DstPath == "" {
			return fail(fmt.Errorf("dst_path is required for move"))
		}
		dstRepo, dstDir, err := s.sf.resolve(ctx, email, in.DstPath)
		if err != nil {
			return fail(err)
		}
		form.Set("dst_repo", dstRepo)
		form.Set("dst_dir", dstDir)
	}

	kind := "file"
	if in.IsDir {
		kind = "dir"
	}
	code, b, err := s.sf.doUser(ctx, email, http.MethodPost,
		"/api2/repos/"+repo+"/"+kind+"/?"+url.Values{"p": {sub}}.Encode(),
		map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
		[]byte(form.Encode()))
	if err != nil {
		return fail(err)
	}
	if code >= 400 {
		return fail(fmt.Errorf("seafile %s http %d: %s", in.Operation, code, snippet(b)))
	}
	return text(string(b))
}

func isText(p string) bool {
	lp := strings.ToLower(p)
	for _, e := range []string{".txt", ".md", ".markdown", ".csv", ".json", ".log", ".xml", ".yml", ".yaml", ".html", ".htm", ".py", ".js", ".ts", ".sh", ".go", ".rs", ".toml", ".ini", ".conf", ".sql"} {
		if strings.HasSuffix(lp, e) {
			return true
		}
	}
	return false
}
