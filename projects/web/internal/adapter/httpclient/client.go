// Package httpclient implements the gateway's upstream port over HTTP/JSON.
package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"labqueue/projects/web/internal/model"
)

type Client struct {
	bases  map[model.Target]*url.URL
	client *http.Client
}

func New(accountsURL, queueURL string, client *http.Client) (*Client, error) {
	if client == nil {
		return nil, errors.New("HTTP client is required")
	}
	bases := make(map[model.Target]*url.URL)
	for target, raw := range map[model.Target]string{model.Accounts: accountsURL, model.Queue: queueURL} {
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
			return nil, errors.New("service URLs must be HTTP(S) origins without a path")
		}
		bases[target] = u
	}
	return &Client{bases: bases, client: client}, nil
}

func (c *Client) request(ctx context.Context, target model.Target, request model.Request) (*http.Response, error) {
	base, ok := c.bases[target]
	if !ok {
		return nil, errors.New("unknown service")
	}
	upstreamURL := *base
	upstreamURL.Path, upstreamURL.RawPath, upstreamURL.RawQuery = request.Path, request.RawPath, request.Query
	req, err := http.NewRequestWithContext(ctx, request.Method, upstreamURL.String(), bytes.NewReader(request.Body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if request.ContentType != "" {
		req.Header.Set("Content-Type", request.ContentType)
	}
	if request.IdempotencyKey != "" {
		req.Header.Set("Idempotency-Key", request.IdempotencyKey)
	}
	if request.Token != "" {
		req.Header.Set("Authorization", "Bearer "+request.Token)
	}
	return c.client.Do(req)
}

func (c *Client) Exchange(ctx context.Context, target model.Target, request model.Request) (model.Response, error) {
	response, err := c.request(ctx, target, request)
	if err != nil {
		return model.Response{}, model.Unavailable()
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
	if err != nil || len(body) > 4<<20 || !json.Valid(body) || response.StatusCode < 200 || response.StatusCode >= 300 && response.StatusCode < 400 {
		return model.Response{}, model.InvalidResponse()
	}
	return model.Response{Status: response.StatusCode, Body: body}, nil
}

type userDTO struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Email   string `json:"email"`
	Role    string `json:"role"`
	GroupID *int64 `json:"group_id"`
}

func (c *Client) Authenticate(ctx context.Context, request model.Request) (model.AuthExchange, error) {
	response, err := c.Exchange(ctx, model.Accounts, request)
	if err != nil {
		return model.AuthExchange{}, err
	}
	exchange := model.AuthExchange{Response: response}
	if response.Status >= 400 {
		return exchange, nil
	}
	var dto struct {
		Token string   `json:"token"`
		User  *userDTO `json:"user"`
	}
	if json.Unmarshal(response.Body, &dto) != nil {
		return model.AuthExchange{}, model.InvalidAuthentication()
	}
	exchange.Token = dto.Token
	if dto.User != nil {
		exchange.User = &model.User{ID: dto.User.ID, Name: dto.User.Name, Email: dto.User.Email, Role: dto.User.Role, GroupID: dto.User.GroupID}
	}
	return exchange, nil
}

func (c *Client) Ready(ctx context.Context, target model.Target) error {
	response, err := c.request(ctx, target, model.Request{Method: "GET", Path: "/readyz"})
	if err != nil {
		return err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode != 200 {
		return fmt.Errorf("service is not ready: status %d", response.StatusCode)
	}
	return nil
}
