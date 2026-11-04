// Package usecase orchestrates browser gateway scenarios through policies.
package usecase

import (
	"context"
	"time"

	"labqueue/projects/web/internal/model"
	"labqueue/projects/web/internal/service"
)

type UseCases struct{ service *service.Service }

func New(service *service.Service) *UseCases { return &UseCases{service: service} }

func (u *UseCases) Routes() []model.Route { return service.Routes() }

func (u *UseCases) Authenticate(ctx context.Context, request model.Request) (model.Result, error) {
	route, err := u.service.Resolve(request, model.Authenticate)
	if err != nil {
		return model.Result{}, err
	}
	request, err = u.service.Prepare(route, request)
	if err != nil {
		return model.Result{}, err
	}
	exchange, err := u.service.Authenticate(ctx, request)
	if err != nil {
		return model.Result{}, err
	}
	return u.service.BrowserAuthentication(exchange)
}

func (u *UseCases) Proxy(ctx context.Context, request model.Request) (model.Response, error) {
	route, err := u.service.Resolve(request, model.Proxy)
	if err != nil {
		return model.Response{}, err
	}
	request, err = u.service.Prepare(route, request)
	if err != nil {
		return model.Response{}, err
	}
	return u.service.Exchange(ctx, route, request)
}

func (u *UseCases) Logout() model.Session { return u.service.Logout() }

func (u *UseCases) Ready(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return u.service.Ready(ctx)
}
