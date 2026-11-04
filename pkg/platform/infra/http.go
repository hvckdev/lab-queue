// Package infra owns concrete network and cryptographic clients shared by the services.
package infra

import (
	"net/http"
	"time"
)

// NewHTTPClient bounds upstream requests and prevents an adapter from following
// a redirect to an unexpected service. Adapters own protocols and DTO mapping.
func NewHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
