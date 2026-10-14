// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"context"
	"errors"
	"net/http"

	"github.com/golang-jwt/jwt/v5"
)

// userHeader is the header Open WebUI signs the caller into when
// ENABLE_FORWARD_USER_INFO_HEADERS and FORWARD_USER_INFO_HEADER_JWT_SECRET are set.
const userHeader = "X-OpenWebUI-User-Jwt"

type ctxKey struct{}

// User is the caller, taken from the verified forwarded JWT.
type User struct {
	ID    string
	Email string
	Name  string
	Role  string
}

// userFrom returns the caller for a request, or nil when unauthenticated.
func userFrom(ctx context.Context) *User {
	u, _ := ctx.Value(ctxKey{}).(*User)
	return u
}

// auth wraps an http.Handler, verifying Open WebUI's forwarded user JWT (HS256)
// and stashing the caller in the request context. The MCP streamable handler
// derives each session from the request context, so tool handlers see it too.
func auth(secret string, next http.Handler) http.Handler {
	key := []byte(secret)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := r.Header.Get(userHeader)
		if raw == "" {
			http.Error(w, "missing "+userHeader, http.StatusUnauthorized)
			return
		}
		claims := jwt.MapClaims{}
		_, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, errors.New("unexpected signing method")
			}
			return key, nil
		})
		if err != nil {
			http.Error(w, "invalid user token", http.StatusUnauthorized)
			return
		}
		u := &User{
			ID:    str(claims["sub"]),
			Email: str(claims["email"]),
			Name:  str(claims["name"]),
			Role:  str(claims["role"]),
		}
		ctx := context.WithValue(r.Context(), ctxKey{}, u)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func str(v any) string {
	s, _ := v.(string)
	return s
}
