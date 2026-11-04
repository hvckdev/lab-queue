package platform

import (
	"context"
	"labqueue/pkg/platform/infra"
	"net/http"
	"strings"
)

const TokenTTL = infra.TokenTTL

type User = infra.User
type Claims = infra.Claims

// Tokens adapts the infrastructure token client to HTTP authentication middleware.
type Tokens struct{ *infra.Tokens }

func NewTokens(secret string) (*Tokens, error) {
	tokens, err := infra.NewTokens(secret)
	if err != nil {
		return nil, err
	}
	return &Tokens{Tokens: tokens}, nil
}

type userKey struct{}

func CurrentUser(ctx context.Context) User { user, _ := ctx.Value(userKey{}).(User); return user }

func (t *Tokens) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			Error(w, 401, "unauthorized", "Войдите в систему")
			return
		}
		user, err := t.Parse(strings.TrimPrefix(header, "Bearer "))
		if err != nil {
			Error(w, 401, "unauthorized", "Сеанс истек. Войдите снова")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey{}, user)))
	})
}
