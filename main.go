// SPDX-License-Identifier: AGPL-3.0-only
//
// openwebui-tools: an MCP server for Open WebUI. It exposes read and write
// tools for Seafile, Mealie, Immich, and Obsidian Fast Note Sync (FNS), acting
// as the caller named by Open WebUI's forwarded user JWT (X-OpenWebUI-User-Jwt,
// HS256). Per-user tokens live in a local store or are minted automatically;
// a shared token per app is the fallback where supported.
package main

import (
	"log"
	"net/http"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const version = "v0.3.0"

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
	FNSURL                 string
}

type server struct {
	cfg   config
	store *tokenStore
	sf    *SeafileClient
	fns   *FNSClient
}

func newServer(cfg config) *server {
	store := newTokenStore(cfg.StorePath)
	return &server{
		cfg:   cfg,
		store: store,
		sf:    newSeafileClient(cfg.SeafileBase, cfg.SeafileAdminEmail, cfg.SeafileAdminPassword, cfg.SeafileAdminToken),
		fns:   newFNSClient(cfg.FNSURL, store),
	}
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
		FNSURL:               os.Getenv("FNS_URL"),
	}
	if cfg.JWTSecret == "" {
		log.Fatal("JWT_SECRET is required: set it to Open WebUI's FORWARD_USER_INFO_HEADER_JWT_SECRET")
	}

	s := newServer(cfg)

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

func (s *server) register(srv *mcp.Server) {
	s.registerAuthTools(srv)
	s.registerSeafileTools(srv)
	s.registerMealieTools(srv)
	s.registerImmichTools(srv)
	s.registerFNSTools(srv)
}

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
