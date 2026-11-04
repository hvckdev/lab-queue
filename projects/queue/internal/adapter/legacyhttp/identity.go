// Package legacyhttp captures protocol identity only at the transport boundary.
// Application and domain layers pass an opaque context and do not depend on it.
package legacyhttp

import (
	"context"
	"net/http"
)

type identity struct{ method, path string }
type identityKey struct{}

func Capture(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), identityKey{}, identity{r.Method, r.URL.Path})))
	})
}
func Identity(ctx context.Context) (method, path string, ok bool) {
	value, ok := ctx.Value(identityKey{}).(identity)
	return value.method, value.path, ok
}
