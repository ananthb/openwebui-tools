// SPDX-License-Identifier: AGPL-3.0-only
//
// openwebui-tools: an MCP server for Open WebUI. It exposes read tools for
// Seafile, Mealie, Immich and Actual, acting as the caller named by Open WebUI's
// forwarded user JWT (X-OpenWebUI-User-Jwt, HS256). Per-user tokens live in a
// local store; a shared token per app is the fallback.
package main

import (
	"log"
	"net/http"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const version = "v0.2.0"

type config struct {
	Addr                   string
	JWTSecret              string
	StorePath              string
	SeafileBase            string
	SeafileAdminEmail      string
	SeafileAdminPassword   string
	SeafileAdminToken      string
	MealieURL, MealieToken string
	ImmichURL, ImmichKey   string
	ActualURL              string
}

type server struct {
	cfg   config
	store *tokenStore
	sf    seafile
}

func main() {
	cfg := config{
		Addr:                 env("ADDR", ":8090"),
		JWTSecret:            os.Getenv("JWT_SECRET"),
		StorePath:            env("STORE_PATH", "/data/tokens.json"),
		SeafileBase:          env("SEAHUB", "http://127.0.0.1"),
		SeafileAdminEmail:    os.Getenv("ADMIN_EMAIL"),
		SeafileAdminPassword: os.Getenv("ADMIN_PASSWORD"),
		SeafileAdminToken:    os.Getenv("ADMIN_TOKEN"),
		MealieURL:            os.Getenv("MEALIE_URL"),
		MealieToken:          os.Getenv("MEALIE_TOKEN"),
		ImmichURL:            os.Getenv("IMMICH_URL"),
		ImmichKey:            os.Getenv("IMMICH_KEY"),
		ActualURL:            os.Getenv("ACTUAL_URL"),
	}
	if cfg.JWTSecret == "" {
		log.Fatal("JWT_SECRET is required: set it to Open WebUI's FORWARD_USER_INFO_HEADER_JWT_SECRET")
	}

	s := &server{cfg: cfg, store: newTokenStore(cfg.StorePath), sf: seafile{userTokens: map[string]string{}}}

	srv := mcp.NewServer(&mcp.Implementation{Name: "openwebui-tools", Version: version}, nil)
	s.register(srv)

	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, nil)
	authed := auth(cfg.JWTSecret, handler)

	mux := http.NewServeMux()
	mux.Handle("/mcp", authed)
	mux.Handle("/mcp/", authed)
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })

	log.Printf("openwebui-tools %s listening on %s", version, cfg.Addr)
	if err := http.ListenAndServe(cfg.Addr, mux); err != nil {
		log.Fatal(err)
	}
}

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
