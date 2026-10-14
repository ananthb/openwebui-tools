// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Empty is the input for tools that take no arguments.
type Empty struct{}

type pathIn struct {
	Path string `json:"path" jsonschema:"Seafile path: 'Library' or 'Library/sub/file.txt'"`
}
type queryIn struct {
	Query string `json:"query" jsonschema:"what to search for"`
}
type slugIn struct {
	Slug string `json:"slug" jsonschema:"recipe slug from mealie_search"`
}
type albumIn struct {
	AlbumID string `json:"album_id" jsonschema:"album id from immich_albums"`
}
type assetIn struct {
	AssetID string `json:"asset_id" jsonschema:"asset id from a search or album"`
}
type accountIn struct {
	AccountID string `json:"account_id" jsonschema:"account id from actual_accounts"`
}
type transactionsIn struct {
	AccountID string `json:"account_id" jsonschema:"account id from actual_accounts"`
	Since     string `json:"since" jsonschema:"ISO date lower bound (optional)"`
}
type monthIn struct {
	Month string `json:"month" jsonschema:"month as YYYY-MM"`
}
type qlIn struct {
	Query string `json:"query" jsonschema:"ActualQL query as JSON"`
}
type tokenIn struct {
	App   string `json:"app" jsonschema:"app name: mealie or immich"`
	Token string `json:"token" jsonschema:"your API token; empty to forget"`
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

// appToken returns the caller's token for an app, falling back to the shared one.
func (s *server) appToken(user *User, app, shared string) string {
	if t, ok := s.store.get(user.ID, app); ok {
		return t
	}
	return shared
}

func (s *server) register(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{Name: "set_token", Description: "Store your personal token for an app (mealie or immich). Call with an empty token to forget it."}, s.setToken)

	mcp.AddTool(srv, &mcp.Tool{Name: "seafile_libraries", Description: "List the Seafile libraries you can access."}, s.seafileLibraries)
	mcp.AddTool(srv, &mcp.Tool{Name: "seafile_browse", Description: "List one Seafile folder. path is 'Library' or 'Library/sub/dir'."}, s.seafileBrowse)
	mcp.AddTool(srv, &mcp.Tool{Name: "seafile_read", Description: "Read a text file from Seafile. path is 'Library/sub/file.txt'."}, s.seafileRead)

	mcp.AddTool(srv, &mcp.Tool{Name: "mealie_search", Description: "Search your Mealie recipes by name or ingredient."}, s.mealieSearch)
	mcp.AddTool(srv, &mcp.Tool{Name: "mealie_recipe", Description: "Fetch one Mealie recipe by slug."}, s.mealieRecipe)
	mcp.AddTool(srv, &mcp.Tool{Name: "mealie_random", Description: "A random Mealie recipe."}, s.mealieRandom)
	mcp.AddTool(srv, &mcp.Tool{Name: "mealie_tags", Description: "List the tags and categories across your recipes."}, s.mealieTags)

	mcp.AddTool(srv, &mcp.Tool{Name: "immich_search", Description: "Search your Immich photos semantically."}, s.immichSearch)
	mcp.AddTool(srv, &mcp.Tool{Name: "immich_albums", Description: "List your Immich albums."}, s.immichAlbums)
	mcp.AddTool(srv, &mcp.Tool{Name: "immich_album", Description: "List the assets in one Immich album."}, s.immichAlbum)
	mcp.AddTool(srv, &mcp.Tool{Name: "immich_asset", Description: "Get one Immich asset's details."}, s.immichAsset)
	mcp.AddTool(srv, &mcp.Tool{Name: "immich_people", Description: "List the people Immich has recognised."}, s.immichPeople)

	mcp.AddTool(srv, &mcp.Tool{Name: "actual_accounts", Description: "List your Actual accounts."}, s.actualAccounts)
	mcp.AddTool(srv, &mcp.Tool{Name: "actual_balance", Description: "The current balance of one Actual account."}, s.actualBalance)
	mcp.AddTool(srv, &mcp.Tool{Name: "actual_transactions", Description: "Recent transactions for one Actual account."}, s.actualTransactions)
	mcp.AddTool(srv, &mcp.Tool{Name: "actual_budget", Description: "Budget vs spent for one month (YYYY-MM)."}, s.actualBudget)
	mcp.AddTool(srv, &mcp.Tool{Name: "actual_query", Description: "Run an ActualQL query (JSON) against the budget."}, s.actualQuery)
}

func (s *server) setToken(ctx context.Context, _ *mcp.CallToolRequest, in tokenIn) (*mcp.CallToolResult, any, error) {
	u := userFrom(ctx)
	if u == nil {
		return nil, nil, fmt.Errorf("unauthenticated")
	}
	if in.App != "mealie" && in.App != "immich" {
		return fail(fmt.Errorf("app must be mealie or immich"))
	}
	if err := s.store.set(u.ID, in.App, strings.TrimSpace(in.Token)); err != nil {
		return fail(err)
	}
	if in.Token == "" {
		return text("forgotten")
	}
	return text("stored")
}

// --- Seafile -----------------------------------------------------------------

func (s *server) seafileToken(ctx context.Context) (string, error) {
	u := userFrom(ctx)
	if u == nil {
		return "", fmt.Errorf("unauthenticated")
	}
	return s.seafileUser(ctx, u.Email)
}

func (s *server) seafileLibraries(ctx context.Context, _ *mcp.CallToolRequest, _ Empty) (*mcp.CallToolResult, any, error) {
	token, err := s.seafileToken(ctx)
	if err != nil {
		return fail(err)
	}
	repos, err := s.seafileRepos(ctx, token)
	if err != nil {
		return fail(err)
	}
	out := make([]map[string]any, 0, len(repos))
	for _, r := range repos {
		out = append(out, map[string]any{"name": r["repo_name"], "size": r["size"]})
	}
	return text(out)
}

func (s *server) seafileBrowse(ctx context.Context, _ *mcp.CallToolRequest, in pathIn) (*mcp.CallToolResult, any, error) {
	token, err := s.seafileToken(ctx)
	if err != nil {
		return fail(err)
	}
	repo, sub, err := s.seafileResolve(ctx, token, in.Path)
	if err != nil {
		return fail(err)
	}
	v, err := getJSON(ctx, s.cfg.SeafileBase+"/api2/repos/"+repo+"/dir/?"+url.Values{"p": {sub}}.Encode(),
		map[string]string{"Authorization": "Token " + token})
	if err != nil {
		return fail(err)
	}
	return text(v)
}

func (s *server) seafileRead(ctx context.Context, _ *mcp.CallToolRequest, in pathIn) (*mcp.CallToolResult, any, error) {
	token, err := s.seafileToken(ctx)
	if err != nil {
		return fail(err)
	}
	repo, sub, err := s.seafileResolve(ctx, token, in.Path)
	if err != nil {
		return fail(err)
	}
	code, b, err := do(ctx, http.MethodGet, s.cfg.SeafileBase+"/api2/repos/"+repo+"/file/?"+url.Values{"p": {sub}}.Encode(),
		map[string]string{"Authorization": "Token " + token}, nil)
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
	u, err := url.Parse(link)
	if err != nil {
		return fail(err)
	}
	target := s.cfg.SeafileBase + u.Path
	if u.RawQuery != "" {
		target += "?" + u.RawQuery
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

func isText(p string) bool {
	lp := strings.ToLower(p)
	for _, e := range []string{".txt", ".md", ".markdown", ".csv", ".json", ".log", ".xml", ".yml", ".yaml", ".html", ".htm", ".py", ".js", ".ts", ".sh", ".go", ".rs", ".toml", ".ini", ".conf", ".sql"} {
		if strings.HasSuffix(lp, e) {
			return true
		}
	}
	return false
}

// --- Mealie ------------------------------------------------------------------

func (s *server) mealieHeaders(ctx context.Context) (map[string]string, error) {
	u := userFrom(ctx)
	if u == nil {
		return nil, fmt.Errorf("unauthenticated")
	}
	tok := s.appToken(u, "mealie", s.cfg.MealieToken)
	if tok == "" {
		return nil, fmt.Errorf("no Mealie token: call set_token(app=\"mealie\", token=...) once")
	}
	return map[string]string{"Authorization": "Bearer " + tok}, nil
}

func (s *server) mealieGet(ctx context.Context, path string) (*mcp.CallToolResult, any, error) {
	h, err := s.mealieHeaders(ctx)
	if err != nil {
		return fail(err)
	}
	v, err := getJSON(ctx, s.cfg.MealieURL+path, h)
	if err != nil {
		return fail(err)
	}
	return text(v)
}

func (s *server) mealieSearch(ctx context.Context, _ *mcp.CallToolRequest, in queryIn) (*mcp.CallToolResult, any, error) {
	return s.mealieGet(ctx, "/api/recipes?"+url.Values{"search": {in.Query}, "perPage": {"10"}}.Encode())
}

func (s *server) mealieRecipe(ctx context.Context, _ *mcp.CallToolRequest, in slugIn) (*mcp.CallToolResult, any, error) {
	return s.mealieGet(ctx, "/api/recipes/"+url.PathEscape(in.Slug))
}

func (s *server) mealieRandom(ctx context.Context, _ *mcp.CallToolRequest, _ Empty) (*mcp.CallToolResult, any, error) {
	return s.mealieGet(ctx, "/api/recipes/random")
}

func (s *server) mealieTags(ctx context.Context, _ *mcp.CallToolRequest, _ Empty) (*mcp.CallToolResult, any, error) {
	h, err := s.mealieHeaders(ctx)
	if err != nil {
		return fail(err)
	}
	tags, err := getJSON(ctx, s.cfg.MealieURL+"/api/organizers/tags", h)
	if err != nil {
		return fail(err)
	}
	cats, err := getJSON(ctx, s.cfg.MealieURL+"/api/organizers/categories", h)
	if err != nil {
		return fail(err)
	}
	return text(map[string]any{"tags": tags, "categories": cats})
}

// --- Immich ------------------------------------------------------------------

func (s *server) immichHeaders(ctx context.Context) (map[string]string, error) {
	u := userFrom(ctx)
	if u == nil {
		return nil, fmt.Errorf("unauthenticated")
	}
	key := s.appToken(u, "immich", s.cfg.ImmichKey)
	if key == "" {
		return nil, fmt.Errorf("no Immich key: call set_token(app=\"immich\", token=...) once")
	}
	return map[string]string{"x-api-key": key}, nil
}

func (s *server) immichGet(ctx context.Context, path string) (*mcp.CallToolResult, any, error) {
	h, err := s.immichHeaders(ctx)
	if err != nil {
		return fail(err)
	}
	v, err := getJSON(ctx, s.cfg.ImmichURL+path, h)
	if err != nil {
		return fail(err)
	}
	return text(v)
}

func (s *server) immichSearch(ctx context.Context, _ *mcp.CallToolRequest, in queryIn) (*mcp.CallToolResult, any, error) {
	h, err := s.immichHeaders(ctx)
	if err != nil {
		return fail(err)
	}
	body, _ := json.Marshal(map[string]any{"query": in.Query, "size": 10, "withExif": true})
	code, b, err := do(ctx, http.MethodPost, s.cfg.ImmichURL+"/api/search/smart", withJSON(h), body)
	if err != nil {
		return fail(err)
	}
	if code >= 400 {
		return fail(fmt.Errorf("immich http %d: %s", code, snippet(b)))
	}
	return text(json.RawMessage(b))
}

func (s *server) immichAlbums(ctx context.Context, _ *mcp.CallToolRequest, _ Empty) (*mcp.CallToolResult, any, error) {
	return s.immichGet(ctx, "/api/albums")
}

func (s *server) immichAlbum(ctx context.Context, _ *mcp.CallToolRequest, in albumIn) (*mcp.CallToolResult, any, error) {
	return s.immichGet(ctx, "/api/albums/"+url.PathEscape(in.AlbumID))
}

func (s *server) immichAsset(ctx context.Context, _ *mcp.CallToolRequest, in assetIn) (*mcp.CallToolResult, any, error) {
	return s.immichGet(ctx, "/api/assets/"+url.PathEscape(in.AssetID))
}

func (s *server) immichPeople(ctx context.Context, _ *mcp.CallToolRequest, _ Empty) (*mcp.CallToolResult, any, error) {
	return s.immichGet(ctx, "/api/people")
}

// --- Actual (via the sidecar) ------------------------------------------------

func (s *server) actualAccounts(ctx context.Context, _ *mcp.CallToolRequest, _ Empty) (*mcp.CallToolResult, any, error) {
	return s.actualGet(ctx, "/accounts")
}

func (s *server) actualBalance(ctx context.Context, _ *mcp.CallToolRequest, in accountIn) (*mcp.CallToolResult, any, error) {
	return s.actualGet(ctx, "/accounts/"+url.PathEscape(in.AccountID)+"/balance")
}

func (s *server) actualTransactions(ctx context.Context, _ *mcp.CallToolRequest, in transactionsIn) (*mcp.CallToolResult, any, error) {
	q := ""
	if in.Since != "" {
		q = "?" + url.Values{"since": {in.Since}}.Encode()
	}
	return s.actualGet(ctx, "/accounts/"+url.PathEscape(in.AccountID)+"/transactions"+q)
}

func (s *server) actualBudget(ctx context.Context, _ *mcp.CallToolRequest, in monthIn) (*mcp.CallToolResult, any, error) {
	return s.actualGet(ctx, "/months/"+url.PathEscape(in.Month)+"/categories")
}

func (s *server) actualQuery(ctx context.Context, _ *mcp.CallToolRequest, in qlIn) (*mcp.CallToolResult, any, error) {
	if !json.Valid([]byte(in.Query)) {
		return fail(fmt.Errorf("query must be valid JSON"))
	}
	body, _ := json.Marshal(map[string]any{"query": json.RawMessage(in.Query)})
	code, b, err := do(ctx, http.MethodPost, s.cfg.ActualURL+"/query", withJSON(nil), body)
	if err != nil {
		return fail(err)
	}
	if code >= 400 {
		return fail(fmt.Errorf("actual http %d: %s", code, snippet(b)))
	}
	return text(json.RawMessage(b))
}

func (s *server) actualGet(ctx context.Context, path string) (*mcp.CallToolResult, any, error) {
	v, err := getJSON(ctx, s.cfg.ActualURL+path, nil)
	if err != nil {
		return fail(err)
	}
	return text(v)
}

func withJSON(h map[string]string) map[string]string {
	out := map[string]string{"Content-Type": "application/json"}
	for k, v := range h {
		out[k] = v
	}
	return out
}
