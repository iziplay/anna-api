package routing

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/golang-jwt/jwt/v5"
)

func authMiddleware(api huma.API) func(ctx huma.Context, next func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		var anyOfNeededScopes []string
		isAuthorizationRequired := false
		for _, opScheme := range ctx.Operation().Security {
			var ok bool
			if anyOfNeededScopes, ok = opScheme["bearerAuth"]; ok {
				isAuthorizationRequired = true
				break
			}
		}
		_ = anyOfNeededScopes // unused, but kept for future scope checking

		if !isAuthorizationRequired {
			next(ctx)
			return
		}

		tokenString := strings.TrimPrefix(ctx.Header("Authorization"), "Bearer ")

		if tokenString == "" {
			tokenString = ctx.Query("jwt")
		}

		secret := os.Getenv("ANNA_JWT_SECRET")
		if secret == "" {
			next(ctx)
			return
		}

		token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return []byte(secret), nil
		})

		if err != nil || !token.Valid {
			huma.WriteErr(api, ctx, http.StatusUnauthorized, "invalid token", err)
			return
		}

		next(ctx)
	}
}
