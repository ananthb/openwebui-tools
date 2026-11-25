// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

func ctxWithUser(id, email, name string) context.Context {
	return context.WithValue(context.Background(), ctxKey{}, &User{
		ID:    id,
		Email: email,
		Name:  name,
		Role:  "user",
	})
}

func resultText(t *testing.T, res any) string {
	t.Helper()
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var parsed struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(b, &parsed); err != nil || len(parsed.Content) == 0 {
		t.Fatalf("unexpected CallToolResult: %s", string(b))
	}
	return parsed.Content[0].Text
}

func TestTokenStatusAndSetToken(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "tokens.json")
	s := newServer(config{
		StorePath:         storePath,
		SeafileBase:       "http://seafile.local",
		SeafileAdminToken: "adm",
		MealieURL:         "http://mealie.local",
		MealieToken:       "shared-mealie",
		ImmichURL:         "http://immich.local",
		FNSURL:            "http://fns.local",
		FNSMintSecret:     "fns-secret",
	})

	ctx := ctxWithUser("u1", "alice@example.com", "Alice")

	// Store personal immich token
	res, _, err := s.setToken(ctx, nil, tokenIn{App: "immich", Token: "alice-immich"})
	if err != nil {
		t.Fatalf("setToken: %v", err)
	}
	if got := resultText(t, res); got != "stored" {
		t.Fatalf("setToken = %q, want stored", got)
	}

	// Check token_status
	res, _, err = s.tokenStatus(ctx, nil, Empty{})
	if err != nil {
		t.Fatalf("tokenStatus: %v", err)
	}
	var status struct {
		User struct {
			ID    string `json:"id"`
			Email string `json:"email"`
			Name  string `json:"name"`
		} `json:"user"`
		Apps struct {
			Seafile struct {
				Configured bool   `json:"configured"`
				Auth       string `json:"auth"`
			} `json:"seafile"`
			FNS struct {
				Configured bool   `json:"configured"`
				Auth       string `json:"auth"`
			} `json:"fns"`
			Mealie struct {
				Configured     bool `json:"configured"`
				PersonalToken  bool `json:"personal_token"`
				SharedFallback bool `json:"shared_fallback"`
			} `json:"mealie"`
			Immich struct {
				Configured     bool `json:"configured"`
				PersonalToken  bool `json:"personal_token"`
				SharedFallback bool `json:"shared_fallback"`
			} `json:"immich"`
		} `json:"apps"`
	}
	if err := json.Unmarshal([]byte(resultText(t, res)), &status); err != nil {
		t.Fatalf("unmarshal tokenStatus: %v", err)
	}
	if status.User.Email != "alice@example.com" || status.User.Name != "Alice" {
		t.Fatalf("unexpected user: %+v", status.User)
	}
	if !status.Apps.Seafile.Configured || status.Apps.Seafile.Auth != "auto" {
		t.Fatalf("unexpected seafile status: %+v", status.Apps.Seafile)
	}
	if !status.Apps.FNS.Configured || status.Apps.FNS.Auth != "auto" {
		t.Fatalf("unexpected fns status: %+v", status.Apps.FNS)
	}
	if !status.Apps.Mealie.Configured || status.Apps.Mealie.PersonalToken || !status.Apps.Mealie.SharedFallback {
		t.Fatalf("unexpected mealie status: %+v", status.Apps.Mealie)
	}
	if !status.Apps.Immich.Configured || !status.Apps.Immich.PersonalToken || status.Apps.Immich.SharedFallback {
		t.Fatalf("unexpected immich status: %+v", status.Apps.Immich)
	}
}

func TestAuthMiddleware(t *testing.T) {
	secret := "test-secret"
	var gotUser *User
	h := auth(secret, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser = userFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":   "user-123",
		"email": "bob@example.com",
		"name":  "Bob",
		"role":  "admin",
	})
	signed, err := tok.SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set(userHeader, signed)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if gotUser == nil || gotUser.Email != "bob@example.com" || gotUser.ID != "user-123" {
		t.Fatalf("unexpected user: %+v", gotUser)
	}
}
