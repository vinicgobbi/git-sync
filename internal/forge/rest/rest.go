// Package rest é um cliente JSON mínimo compartilhado pelos clientes de forge.
package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/vinicgobbi/git-sync/internal/forge"
)

type Client struct {
	BaseURL    string // ex.: https://git.exemplo.com/api/v1
	AuthHeader string // ex.: "token abc" ou "Bearer abc"
	HTTP       *http.Client
}

func New(baseURL, authHeader string) *Client {
	return &Client{BaseURL: baseURL, AuthHeader: authHeader, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

// Do executa a requisição. body e out podem ser nil.
// 404 vira forge.ErrNotFound; outros status >= 400 viram *forge.APIError.
func (c *Client) Do(ctx context.Context, method, path string, body, out any) error {
	_, err := c.DoHeader(ctx, method, path, body, out)
	return err
}

// DoHeader é o Do que devolve também os cabeçalhos da resposta.
func (c *Client) DoHeader(ctx context.Context, method, path string, body, out any) (http.Header, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "git-sync")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.AuthHeader != "" {
		req.Header.Set("Authorization", c.AuthHeader)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return resp.Header, forge.ErrNotFound
	}
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return resp.Header, &forge.APIError{Status: resp.StatusCode, Method: method, Path: path, Body: string(b),
			Accepted: accepted(resp.Header)}
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return resp.Header, nil
	}
	return resp.Header, json.NewDecoder(resp.Body).Decode(out)
}

// accepted lê o que o GitHub diz que a operação aceita: escopos de token
// classic ou permissões de fine-grained. "" fora do GitHub.
func accepted(h http.Header) string {
	if v := strings.TrimSpace(h.Get("X-Accepted-OAuth-Scopes")); v != "" {
		return "escopos " + v
	}
	if v := strings.TrimSpace(h.Get("X-Accepted-GitHub-Permissions")); v != "" {
		return "permissões " + v
	}
	return ""
}
