// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestImmichWriteTools(t *testing.T) {
	type call struct {
		Method string
		Path   string
		Body   string
	}
	var calls []call

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "im-key" {
			http.Error(w, "bad key", http.StatusUnauthorized)
			return
		}
		b, _ := io.ReadAll(r.Body)
		calls = append(calls, call{r.Method, r.URL.Path, string(b)})
		if r.Method == http.MethodDelete && r.URL.Path == "/api/assets" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	s := newServer(config{
		StorePath: filepath.Join(t.TempDir(), "tokens.json"),
		ImmichURL: srv.URL,
		ImmichKey: "im-key",
	})
	ctx := ctxWithUser("u1", "alice@example.com", "Alice")

	_, _, _ = s.immichCreateAlbum(ctx, nil, immichCreateAlbumIn{AlbumName: "Trip", AssetIDs: []string{"a1"}})
	_, _, _ = s.immichUpdateAlbum(ctx, nil, immichUpdateAlbumIn{AlbumID: "alb1", AlbumName: strPtrIfNotEmpty("Trip 2")})
	_, _, _ = s.immichAlbumAssets(ctx, nil, immichAlbumAssetsIn{AlbumID: "alb1", Action: "add", IDs: []string{"a2"}})
	_, _, _ = s.immichAlbumAssets(ctx, nil, immichAlbumAssetsIn{AlbumID: "alb1", Action: "remove", IDs: []string{"a1"}})
	fav := true
	_, _, _ = s.immichUpdateAsset(ctx, nil, immichUpdateAssetIn{AssetID: "a2", IsFavorite: &fav})
	res, _, err := s.immichDeleteAssets(ctx, nil, immichDeleteAssetsIn{IDs: []string{"a1"}, Force: true})
	if err != nil {
		t.Fatalf("immichDeleteAssets: %v", err)
	}
	if got := resultText(t, res); got != "ok" {
		t.Fatalf("delete assets 204 result = %q, want ok", got)
	}
	_, _, _ = s.immichTags(ctx, nil, Empty{})
	_, _, _ = s.immichTagAssets(ctx, nil, immichTagAssetsIn{TagID: "t1", IDs: []string{"a2"}})

	want := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/albums"},
		{http.MethodPatch, "/api/albums/alb1"},
		{http.MethodPut, "/api/albums/alb1/assets"},
		{http.MethodDelete, "/api/albums/alb1/assets"},
		{http.MethodPut, "/api/assets/a2"},
		{http.MethodDelete, "/api/assets"},
		{http.MethodGet, "/api/tags"},
		{http.MethodPost, "/api/tags/t1/assets"},
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

func TestMealieWriteTools(t *testing.T) {
	type call struct {
		Method string
		Path   string
		Body   string
	}
	var calls []call

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer ml-tok" {
			http.Error(w, "bad token", http.StatusUnauthorized)
			return
		}
		b, _ := io.ReadAll(r.Body)
		calls = append(calls, call{r.Method, r.URL.Path, string(b)})
		if r.Method == http.MethodPost && r.URL.Path == "/api/recipes" {
			_, _ = w.Write([]byte(`"pasta"`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	s := newServer(config{
		StorePath:   filepath.Join(t.TempDir(), "tokens.json"),
		MealieURL:   srv.URL,
		MealieToken: "ml-tok",
	})
	ctx := ctxWithUser("u1", "alice@example.com", "Alice")

	_, _, _ = s.mealieCreateRecipe(ctx, nil, mealieCreateRecipeIn{
		Name:         "Pasta",
		Description:  "Quick pasta",
		Ingredients:  []string{"200g spaghetti"},
		Instructions: []string{"Boil water"},
	})
	_, _, _ = s.mealieImportRecipeURL(ctx, nil, mealieImportURLIn{URL: "https://example.com/r", IncludeTags: true})
	_, _, _ = s.mealieUpdateRecipe(ctx, nil, mealieUpdateRecipeIn{Slug: "pasta", Description: strPtrIfNotEmpty("Updated")})
	_, _, _ = s.mealieDeleteRecipe(ctx, nil, mealieSlugIn{Slug: "pasta"})
	_, _, _ = s.mealieMealplans(ctx, nil, mealieMealplansIn{Action: "list", StartDate: "2026-11-25"})
	_, _, _ = s.mealieMealplans(ctx, nil, mealieMealplansIn{Action: "create", Date: "2026-11-25", EntryType: "dinner", Title: "Pasta"})
	_, _, _ = s.mealieMealplans(ctx, nil, mealieMealplansIn{Action: "delete", ID: "mp1"})
	_, _, _ = s.mealieShoppingLists(ctx, nil, Empty{})
	_, _, _ = s.mealieShoppingAddItem(ctx, nil, mealieShoppingAddIn{ShoppingListID: "sl1", Note: "Tomatoes"})
	checked := true
	_, _, _ = s.mealieShoppingUpdateItem(ctx, nil, mealieShoppingUpdateIn{ItemID: "it1", ShoppingListID: "sl1", Checked: &checked})

	want := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/recipes"},
		{http.MethodPatch, "/api/recipes/pasta"},
		{http.MethodPost, "/api/recipes/create/url"},
		{http.MethodPatch, "/api/recipes/pasta"},
		{http.MethodDelete, "/api/recipes/pasta"},
		{http.MethodGet, "/api/households/mealplans"},
		{http.MethodPost, "/api/households/mealplans"},
		{http.MethodDelete, "/api/households/mealplans/mp1"},
		{http.MethodGet, "/api/households/shopping/lists"},
		{http.MethodPost, "/api/households/shopping/items"},
		{http.MethodPut, "/api/households/shopping/items/it1"},
	}
	if len(calls) != len(want) {
		t.Fatalf("got %d calls, want %d", len(calls), len(want))
	}
	for i, w := range want {
		if calls[i].Method != w.method || calls[i].Path != w.path {
			t.Fatalf("call[%d] = %s %s, want %s %s", i, calls[i].Method, calls[i].Path, w.method, w.path)
		}
	}
	if !strings.Contains(calls[1].Body, "200g spaghetti") || !strings.Contains(calls[1].Body, "Boil water") {
		t.Fatalf("patch body = %s", calls[1].Body)
	}
}
