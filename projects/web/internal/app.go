// Package app is the composition root for the browser gateway.
package app

import (
	"net/http"
	"time"

	"labqueue/pkg/platform/infra"
	"labqueue/projects/web/internal/adapter/httpclient"
	"labqueue/projects/web/internal/handler"
	"labqueue/projects/web/internal/service"
	"labqueue/projects/web/internal/usecase"
)

// New wires the HTTP adapter, policies, use cases, and browser handler.
func New(accountsURL, queueURL, publicOrigin string, cookieSecure bool) (http.Handler, error) {
	upstream, err := httpclient.New(accountsURL, queueURL, infra.NewHTTPClient(8*time.Second))
	if err != nil {
		return nil, err
	}
	return handler.New(usecase.New(service.New(upstream)), publicOrigin, cookieSecure, assets)
}
