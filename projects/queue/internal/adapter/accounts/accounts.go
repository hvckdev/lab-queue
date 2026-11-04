package accounts

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"labqueue/projects/queue/internal/model"
)

type Client struct {
	base string
	http *http.Client
}

func New(base string, client *http.Client) *Client {
	return &Client{base: strings.TrimRight(base, "/"), http: client}
}

func (c *Client) Group(ctx context.Context, id int64) (model.Group, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/api/groups/%d", c.base, id), nil)
	if err != nil {
		return model.Group{}, err
	}
	response, err := c.http.Do(req)
	if err != nil {
		return model.Group{}, model.Problem(model.Unavailable, "accounts_unavailable", "Сервис групп временно недоступен")
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return model.Group{}, model.Problem(model.Invalid, "invalid_group", "Группа не найдена")
	}
	if response.StatusCode != http.StatusOK {
		return model.Group{}, model.Problem(model.Unavailable, "accounts_unavailable", "Сервис групп временно недоступен")
	}
	var payload struct {
		Group struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"group"`
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&payload); err != nil || payload.Group.ID != id || payload.Group.Name == "" {
		return model.Group{}, model.Problem(model.Unavailable, "accounts_unavailable", "Сервис групп вернул некорректный ответ")
	}
	return model.Group{ID: payload.Group.ID, Name: payload.Group.Name}, nil
}
