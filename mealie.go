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

type mealieQueryIn struct {
	Query string `json:"query" jsonschema:"what to search for"`
}

type mealieSlugIn struct {
	Slug string `json:"slug" jsonschema:"recipe slug from mealie_search"`
}

type mealieCreateRecipeIn struct {
	Name         string   `json:"name" jsonschema:"recipe name"`
	Description  string   `json:"description,omitempty" jsonschema:"recipe description"`
	Ingredients  []string `json:"ingredients,omitempty" jsonschema:"list of ingredients as strings"`
	Instructions []string `json:"instructions,omitempty" jsonschema:"list of instruction steps"`
	Tags         []string `json:"tags,omitempty" jsonschema:"tag names"`
	Notes        []string `json:"notes,omitempty" jsonschema:"recipe notes"`
}

type mealieImportURLIn struct {
	URL         string `json:"url" jsonschema:"URL of the recipe to import"`
	IncludeTags bool   `json:"include_tags,omitempty" jsonschema:"whether to import tags from the source page"`
}

type mealieUpdateRecipeIn struct {
	Slug         string   `json:"slug" jsonschema:"recipe slug"`
	Name         *string  `json:"name,omitempty" jsonschema:"new recipe name"`
	Description  *string  `json:"description,omitempty" jsonschema:"new recipe description"`
	Ingredients  []string `json:"ingredients,omitempty" jsonschema:"replacement list of ingredients"`
	Instructions []string `json:"instructions,omitempty" jsonschema:"replacement list of instruction steps"`
	Notes        []string `json:"notes,omitempty" jsonschema:"replacement list of notes"`
}

type mealieMealplansIn struct {
	Action    string `json:"action" jsonschema:"'list', 'create', or 'delete'"`
	ID        string `json:"id,omitempty" jsonschema:"mealplan entry id (for delete)"`
	StartDate string `json:"start_date,omitempty" jsonschema:"YYYY-MM-DD (for list)"`
	EndDate   string `json:"end_date,omitempty" jsonschema:"YYYY-MM-DD (for list)"`
	Date      string `json:"date,omitempty" jsonschema:"YYYY-MM-DD (for create)"`
	EntryType string `json:"entry_type,omitempty" jsonschema:"breakfast, lunch, dinner, or side (for create)"`
	RecipeID  string `json:"recipe_id,omitempty" jsonschema:"recipe UUID (optional, for create)"`
	Title     string `json:"title,omitempty" jsonschema:"entry title (optional, for create)"`
	Text      string `json:"text,omitempty" jsonschema:"entry note text (optional, for create)"`
}

type mealieShoppingAddIn struct {
	ShoppingListID string   `json:"shopping_list_id" jsonschema:"shopping list UUID from mealie_shopping_lists"`
	Note           string   `json:"note" jsonschema:"item name/note"`
	Quantity       *float64 `json:"quantity,omitempty" jsonschema:"optional quantity"`
	Display        string   `json:"display,omitempty" jsonschema:"optional display text"`
}

type mealieShoppingUpdateIn struct {
	ItemID         string   `json:"item_id" jsonschema:"shopping list item UUID"`
	ShoppingListID string   `json:"shopping_list_id" jsonschema:"shopping list UUID"`
	Note           *string  `json:"note,omitempty" jsonschema:"updated item note"`
	Quantity       *float64 `json:"quantity,omitempty" jsonschema:"updated quantity"`
	Checked        *bool    `json:"checked,omitempty" jsonschema:"mark item checked/unchecked"`
}

func (s *server) registerMealieTools(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{Name: "mealie_search", Description: "Search your Mealie recipes by name or ingredient."}, s.mealieSearch)
	mcp.AddTool(srv, &mcp.Tool{Name: "mealie_recipe", Description: "Fetch one Mealie recipe by slug."}, s.mealieRecipe)
	mcp.AddTool(srv, &mcp.Tool{Name: "mealie_random", Description: "A random Mealie recipe."}, s.mealieRandom)
	mcp.AddTool(srv, &mcp.Tool{Name: "mealie_tags", Description: "List the tags and categories across your recipes."}, s.mealieTags)
	mcp.AddTool(srv, &mcp.Tool{Name: "mealie_create_recipe", Description: "Create a new Mealie recipe with optional description, ingredients, instructions, tags, and notes."}, s.mealieCreateRecipe)
	mcp.AddTool(srv, &mcp.Tool{Name: "mealie_import_recipe_url", Description: "Import a Mealie recipe from a URL."}, s.mealieImportRecipeURL)
	mcp.AddTool(srv, &mcp.Tool{Name: "mealie_update_recipe", Description: "Update an existing Mealie recipe by slug."}, s.mealieUpdateRecipe)
	mcp.AddTool(srv, &mcp.Tool{Name: "mealie_delete_recipe", Description: "Delete a Mealie recipe by slug."}, s.mealieDeleteRecipe)
	mcp.AddTool(srv, &mcp.Tool{Name: "mealie_mealplans", Description: "List, create, or delete Mealie meal plans."}, s.mealieMealplans)
	mcp.AddTool(srv, &mcp.Tool{Name: "mealie_shopping_lists", Description: "List your Mealie shopping lists."}, s.mealieShoppingLists)
	mcp.AddTool(srv, &mcp.Tool{Name: "mealie_shopping_add_item", Description: "Add an item to a Mealie shopping list."}, s.mealieShoppingAddItem)
	mcp.AddTool(srv, &mcp.Tool{Name: "mealie_shopping_update_item", Description: "Update an item on a Mealie shopping list."}, s.mealieShoppingUpdateItem)
}

func (s *server) mealieHeaders(ctx context.Context) (map[string]string, error) {
	u := userFrom(ctx)
	if u == nil {
		return nil, fmt.Errorf("unauthenticated")
	}
	if s.cfg.MealieURL == "" {
		return nil, fmt.Errorf("MEALIE_URL is not configured")
	}
	tok := s.appToken(u, "mealie", s.cfg.MealieToken)
	if tok == "" {
		return nil, fmt.Errorf("no Mealie token: call set_token(app=\"mealie\", token=...) once")
	}
	return map[string]string{"Authorization": "Bearer " + tok}, nil
}

func (s *server) mealieGet(ctx context.Context, apiPath string) (*mcp.CallToolResult, any, error) {
	h, err := s.mealieHeaders(ctx)
	if err != nil {
		return fail(err)
	}
	v, err := getJSON(ctx, strings.TrimRight(s.cfg.MealieURL, "/")+apiPath, h)
	if err != nil {
		return fail(err)
	}
	return text(v)
}

func (s *server) mealieDo(ctx context.Context, method, apiPath string, payload any) (*mcp.CallToolResult, any, error) {
	h, err := s.mealieHeaders(ctx)
	if err != nil {
		return fail(err)
	}
	code, b, err := doJSON(ctx, method, strings.TrimRight(s.cfg.MealieURL, "/")+apiPath, h, payload)
	if err != nil {
		return fail(err)
	}
	if code >= 400 {
		return fail(fmt.Errorf("mealie http %d: %s", code, snippet(b)))
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return text("ok")
	}
	if json.Valid(b) {
		return text(json.RawMessage(b))
	}
	return text(string(b))
}

func (s *server) mealieSearch(ctx context.Context, _ *mcp.CallToolRequest, in mealieQueryIn) (*mcp.CallToolResult, any, error) {
	return s.mealieGet(ctx, "/api/recipes?"+url.Values{"search": {in.Query}, "perPage": {"10"}}.Encode())
}

func (s *server) mealieRecipe(ctx context.Context, _ *mcp.CallToolRequest, in mealieSlugIn) (*mcp.CallToolResult, any, error) {
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
	base := strings.TrimRight(s.cfg.MealieURL, "/")
	tags, err := getJSON(ctx, base+"/api/organizers/tags", h)
	if err != nil {
		return fail(err)
	}
	cats, err := getJSON(ctx, base+"/api/organizers/categories", h)
	if err != nil {
		return fail(err)
	}
	return text(map[string]any{"tags": tags, "categories": cats})
}

func (s *server) mealieCreateRecipe(ctx context.Context, _ *mcp.CallToolRequest, in mealieCreateRecipeIn) (*mcp.CallToolResult, any, error) {
	h, err := s.mealieHeaders(ctx)
	if err != nil {
		return fail(err)
	}
	base := strings.TrimRight(s.cfg.MealieURL, "/")
	code, b, err := doJSON(ctx, http.MethodPost, base+"/api/recipes", h, map[string]any{"name": in.Name})
	if err != nil {
		return fail(err)
	}
	if code >= 400 {
		return fail(fmt.Errorf("mealie create http %d: %s", code, snippet(b)))
	}
	var slug string
	if err := json.Unmarshal(b, &slug); err != nil {
		slug = strings.Trim(strings.TrimSpace(string(b)), `"`)
	}

	patch := buildRecipePatch(nil, strPtrIfNotEmpty(in.Description), in.Ingredients, in.Instructions, in.Tags, in.Notes)
	if len(patch) == 0 {
		return text(map[string]any{"slug": slug})
	}
	return s.mealieDo(ctx, http.MethodPatch, "/api/recipes/"+url.PathEscape(slug), patch)
}

func (s *server) mealieImportRecipeURL(ctx context.Context, _ *mcp.CallToolRequest, in mealieImportURLIn) (*mcp.CallToolResult, any, error) {
	return s.mealieDo(ctx, http.MethodPost, "/api/recipes/create/url", map[string]any{
		"url":         in.URL,
		"includeTags": in.IncludeTags,
	})
}

func (s *server) mealieUpdateRecipe(ctx context.Context, _ *mcp.CallToolRequest, in mealieUpdateRecipeIn) (*mcp.CallToolResult, any, error) {
	patch := buildRecipePatch(in.Name, in.Description, in.Ingredients, in.Instructions, nil, in.Notes)
	return s.mealieDo(ctx, http.MethodPatch, "/api/recipes/"+url.PathEscape(in.Slug), patch)
}

func (s *server) mealieDeleteRecipe(ctx context.Context, _ *mcp.CallToolRequest, in mealieSlugIn) (*mcp.CallToolResult, any, error) {
	return s.mealieDo(ctx, http.MethodDelete, "/api/recipes/"+url.PathEscape(in.Slug), nil)
}

func (s *server) mealieMealplans(ctx context.Context, _ *mcp.CallToolRequest, in mealieMealplansIn) (*mcp.CallToolResult, any, error) {
	switch in.Action {
	case "list":
		q := url.Values{}
		if in.StartDate != "" {
			q.Set("start_date", in.StartDate)
		}
		if in.EndDate != "" {
			q.Set("end_date", in.EndDate)
		}
		apiPath := "/api/households/mealplans"
		if enc := q.Encode(); enc != "" {
			apiPath += "?" + enc
		}
		return s.mealieGet(ctx, apiPath)
	case "create":
		payload := map[string]any{
			"date":      in.Date,
			"entryType": in.EntryType,
		}
		if in.RecipeID != "" {
			payload["recipeId"] = in.RecipeID
		}
		if in.Title != "" {
			payload["title"] = in.Title
		}
		if in.Text != "" {
			payload["text"] = in.Text
		}
		return s.mealieDo(ctx, http.MethodPost, "/api/households/mealplans", payload)
	case "delete":
		if in.ID == "" {
			return fail(fmt.Errorf("id is required for delete"))
		}
		return s.mealieDo(ctx, http.MethodDelete, "/api/households/mealplans/"+url.PathEscape(in.ID), nil)
	default:
		return fail(fmt.Errorf("action must be 'list', 'create', or 'delete'"))
	}
}

func (s *server) mealieShoppingLists(ctx context.Context, _ *mcp.CallToolRequest, _ Empty) (*mcp.CallToolResult, any, error) {
	return s.mealieGet(ctx, "/api/households/shopping/lists")
}

func (s *server) mealieShoppingAddItem(ctx context.Context, _ *mcp.CallToolRequest, in mealieShoppingAddIn) (*mcp.CallToolResult, any, error) {
	payload := map[string]any{
		"shoppingListId": in.ShoppingListID,
		"note":           in.Note,
	}
	if in.Quantity != nil {
		payload["quantity"] = *in.Quantity
	}
	if in.Display != "" {
		payload["display"] = in.Display
	}
	return s.mealieDo(ctx, http.MethodPost, "/api/households/shopping/items", payload)
}

func (s *server) mealieShoppingUpdateItem(ctx context.Context, _ *mcp.CallToolRequest, in mealieShoppingUpdateIn) (*mcp.CallToolResult, any, error) {
	payload := map[string]any{
		"shoppingListId": in.ShoppingListID,
	}
	if in.Note != nil {
		payload["note"] = *in.Note
	}
	if in.Quantity != nil {
		payload["quantity"] = *in.Quantity
	}
	if in.Checked != nil {
		payload["checked"] = *in.Checked
	}
	return s.mealieDo(ctx, http.MethodPut, "/api/households/shopping/items/"+url.PathEscape(in.ItemID), payload)
}

func strPtrIfNotEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func buildRecipePatch(name, desc *string, ingredients, instructions, tags, notes []string) map[string]any {
	patch := map[string]any{}
	if name != nil {
		patch["name"] = *name
	}
	if desc != nil {
		patch["description"] = *desc
	}
	if len(ingredients) > 0 {
		items := make([]map[string]any, 0, len(ingredients))
		for _, ing := range ingredients {
			items = append(items, map[string]any{"note": ing})
		}
		patch["recipeIngredient"] = items
	}
	if len(instructions) > 0 {
		steps := make([]map[string]any, 0, len(instructions))
		for _, step := range instructions {
			steps = append(steps, map[string]any{"text": step})
		}
		patch["recipeInstructions"] = steps
	}
	if len(tags) > 0 {
		t := make([]map[string]any, 0, len(tags))
		for _, tag := range tags {
			t = append(t, map[string]any{"name": tag})
		}
		patch["tags"] = t
	}
	if len(notes) > 0 {
		n := make([]map[string]any, 0, len(notes))
		for _, note := range notes {
			n = append(n, map[string]any{"title": "", "text": note})
		}
		patch["notes"] = n
	}
	return patch
}
