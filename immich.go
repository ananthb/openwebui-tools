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

type immichQueryIn struct {
	Query string `json:"query" jsonschema:"what to search for"`
}

type immichAlbumIn struct {
	AlbumID string `json:"album_id" jsonschema:"album id from immich_albums"`
}

type immichAssetIn struct {
	AssetID string `json:"asset_id" jsonschema:"asset id from a search or album"`
}

type immichCreateAlbumIn struct {
	AlbumName   string   `json:"album_name" jsonschema:"name of the new album"`
	Description string   `json:"description,omitempty" jsonschema:"optional album description"`
	AssetIDs    []string `json:"asset_ids,omitempty" jsonschema:"optional initial asset IDs"`
}

type immichUpdateAlbumIn struct {
	AlbumID     string  `json:"album_id" jsonschema:"album id from immich_albums"`
	AlbumName   *string `json:"album_name,omitempty" jsonschema:"new album name"`
	Description *string `json:"description,omitempty" jsonschema:"new album description"`
}

type immichAlbumAssetsIn struct {
	AlbumID string   `json:"album_id" jsonschema:"album id from immich_albums"`
	Action  string   `json:"action" jsonschema:"'add' or 'remove'"`
	IDs     []string `json:"ids" jsonschema:"asset IDs to add or remove"`
}

type immichUpdateAssetIn struct {
	AssetID          string  `json:"asset_id" jsonschema:"asset id"`
	IsFavorite       *bool   `json:"is_favorite,omitempty" jsonschema:"mark or unmark as favorite"`
	Description      *string `json:"description,omitempty" jsonschema:"asset description"`
	DateTimeOriginal *string `json:"date_time_original,omitempty" jsonschema:"ISO 8601 timestamp"`
	Visibility       *string `json:"visibility,omitempty" jsonschema:"archive, timeline, hidden, or locked"`
}

type immichDeleteAssetsIn struct {
	IDs   []string `json:"ids" jsonschema:"asset IDs to delete"`
	Force bool     `json:"force,omitempty" jsonschema:"if true, permanently delete instead of moving to trash"`
}

type immichTagAssetsIn struct {
	TagID string   `json:"tag_id" jsonschema:"tag id from immich_tags"`
	IDs   []string `json:"ids" jsonschema:"asset IDs to tag"`
}

func (s *server) registerImmichTools(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{Name: "immich_search", Description: "Search your Immich photos semantically."}, s.immichSearch)
	mcp.AddTool(srv, &mcp.Tool{Name: "immich_albums", Description: "List your Immich albums."}, s.immichAlbums)
	mcp.AddTool(srv, &mcp.Tool{Name: "immich_album", Description: "List the assets in one Immich album."}, s.immichAlbum)
	mcp.AddTool(srv, &mcp.Tool{Name: "immich_asset", Description: "Get one Immich asset's details."}, s.immichAsset)
	mcp.AddTool(srv, &mcp.Tool{Name: "immich_people", Description: "List the people Immich has recognised."}, s.immichPeople)
	mcp.AddTool(srv, &mcp.Tool{Name: "immich_create_album", Description: "Create a new Immich album."}, s.immichCreateAlbum)
	mcp.AddTool(srv, &mcp.Tool{Name: "immich_update_album", Description: "Update an Immich album's name or description."}, s.immichUpdateAlbum)
	mcp.AddTool(srv, &mcp.Tool{Name: "immich_album_assets", Description: "Add or remove assets from an Immich album."}, s.immichAlbumAssets)
	mcp.AddTool(srv, &mcp.Tool{Name: "immich_update_asset", Description: "Update an Immich asset's metadata (favorite, description, dateTimeOriginal, visibility)."}, s.immichUpdateAsset)
	mcp.AddTool(srv, &mcp.Tool{Name: "immich_delete_assets", Description: "Delete Immich assets (to trash, or permanently with force=true)."}, s.immichDeleteAssets)
	mcp.AddTool(srv, &mcp.Tool{Name: "immich_tags", Description: "List your Immich tags."}, s.immichTags)
	mcp.AddTool(srv, &mcp.Tool{Name: "immich_tag_assets", Description: "Apply a tag to Immich assets."}, s.immichTagAssets)
}

func (s *server) immichHeaders(ctx context.Context) (map[string]string, error) {
	u := userFrom(ctx)
	if u == nil {
		return nil, fmt.Errorf("unauthenticated")
	}
	if s.cfg.ImmichURL == "" {
		return nil, fmt.Errorf("IMMICH_URL is not configured")
	}
	key := s.appToken(u, "immich", s.cfg.ImmichKey)
	if key == "" {
		return nil, fmt.Errorf("no Immich key: call set_token(app=\"immich\", token=...) once")
	}
	return map[string]string{"x-api-key": key}, nil
}

func (s *server) immichGet(ctx context.Context, apiPath string) (*mcp.CallToolResult, any, error) {
	h, err := s.immichHeaders(ctx)
	if err != nil {
		return fail(err)
	}
	v, err := getJSON(ctx, strings.TrimRight(s.cfg.ImmichURL, "/")+apiPath, h)
	if err != nil {
		return fail(err)
	}
	return text(v)
}

func (s *server) immichDo(ctx context.Context, method, apiPath string, payload any) (*mcp.CallToolResult, any, error) {
	h, err := s.immichHeaders(ctx)
	if err != nil {
		return fail(err)
	}
	code, b, err := doJSON(ctx, method, strings.TrimRight(s.cfg.ImmichURL, "/")+apiPath, h, payload)
	if err != nil {
		return fail(err)
	}
	if code >= 400 {
		return fail(fmt.Errorf("immich http %d: %s", code, snippet(b)))
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return text("ok")
	}
	return text(json.RawMessage(b))
}

func (s *server) immichSearch(ctx context.Context, _ *mcp.CallToolRequest, in immichQueryIn) (*mcp.CallToolResult, any, error) {
	return s.immichDo(ctx, http.MethodPost, "/api/search/smart", map[string]any{
		"query":    in.Query,
		"size":     10,
		"withExif": true,
	})
}

func (s *server) immichAlbums(ctx context.Context, _ *mcp.CallToolRequest, _ Empty) (*mcp.CallToolResult, any, error) {
	return s.immichGet(ctx, "/api/albums")
}

func (s *server) immichAlbum(ctx context.Context, _ *mcp.CallToolRequest, in immichAlbumIn) (*mcp.CallToolResult, any, error) {
	return s.immichGet(ctx, "/api/albums/"+url.PathEscape(in.AlbumID))
}

func (s *server) immichAsset(ctx context.Context, _ *mcp.CallToolRequest, in immichAssetIn) (*mcp.CallToolResult, any, error) {
	return s.immichGet(ctx, "/api/assets/"+url.PathEscape(in.AssetID))
}

func (s *server) immichPeople(ctx context.Context, _ *mcp.CallToolRequest, _ Empty) (*mcp.CallToolResult, any, error) {
	return s.immichGet(ctx, "/api/people")
}

func (s *server) immichCreateAlbum(ctx context.Context, _ *mcp.CallToolRequest, in immichCreateAlbumIn) (*mcp.CallToolResult, any, error) {
	payload := map[string]any{"albumName": in.AlbumName}
	if in.Description != "" {
		payload["description"] = in.Description
	}
	if len(in.AssetIDs) > 0 {
		payload["assetIds"] = in.AssetIDs
	}
	return s.immichDo(ctx, http.MethodPost, "/api/albums", payload)
}

func (s *server) immichUpdateAlbum(ctx context.Context, _ *mcp.CallToolRequest, in immichUpdateAlbumIn) (*mcp.CallToolResult, any, error) {
	payload := map[string]any{}
	if in.AlbumName != nil {
		payload["albumName"] = *in.AlbumName
	}
	if in.Description != nil {
		payload["description"] = *in.Description
	}
	return s.immichDo(ctx, http.MethodPatch, "/api/albums/"+url.PathEscape(in.AlbumID), payload)
}

func (s *server) immichAlbumAssets(ctx context.Context, _ *mcp.CallToolRequest, in immichAlbumAssetsIn) (*mcp.CallToolResult, any, error) {
	var method string
	switch in.Action {
	case "add":
		method = http.MethodPut
	case "remove":
		method = http.MethodDelete
	default:
		return fail(fmt.Errorf("action must be 'add' or 'remove'"))
	}
	return s.immichDo(ctx, method, "/api/albums/"+url.PathEscape(in.AlbumID)+"/assets", map[string]any{"ids": in.IDs})
}

func (s *server) immichUpdateAsset(ctx context.Context, _ *mcp.CallToolRequest, in immichUpdateAssetIn) (*mcp.CallToolResult, any, error) {
	payload := map[string]any{}
	if in.IsFavorite != nil {
		payload["isFavorite"] = *in.IsFavorite
	}
	if in.Description != nil {
		payload["description"] = *in.Description
	}
	if in.DateTimeOriginal != nil {
		payload["dateTimeOriginal"] = *in.DateTimeOriginal
	}
	if in.Visibility != nil {
		payload["visibility"] = *in.Visibility
	}
	return s.immichDo(ctx, http.MethodPut, "/api/assets/"+url.PathEscape(in.AssetID), payload)
}

func (s *server) immichDeleteAssets(ctx context.Context, _ *mcp.CallToolRequest, in immichDeleteAssetsIn) (*mcp.CallToolResult, any, error) {
	payload := map[string]any{"ids": in.IDs}
	if in.Force {
		payload["force"] = true
	}
	return s.immichDo(ctx, http.MethodDelete, "/api/assets", payload)
}

func (s *server) immichTags(ctx context.Context, _ *mcp.CallToolRequest, _ Empty) (*mcp.CallToolResult, any, error) {
	return s.immichGet(ctx, "/api/tags")
}

func (s *server) immichTagAssets(ctx context.Context, _ *mcp.CallToolRequest, in immichTagAssetsIn) (*mcp.CallToolResult, any, error) {
	return s.immichDo(ctx, http.MethodPost, "/api/tags/"+url.PathEscape(in.TagID)+"/assets", map[string]any{"ids": in.IDs})
}
