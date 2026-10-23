// Package github implementa forge.Forge para GitHub (apenas como destino).
package github

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/vinicgobbi/git-sync/internal/forge"
	"github.com/vinicgobbi/git-sync/internal/forge/rest"
)

const pageSize = 100

type Client struct {
	api *rest.Client

	userOnce sync.Once
	user     string
	userErr  error
}

var _ forge.Forge = (*Client)(nil)

// New cria um cliente. baseURL é https://github.com ou a URL de um GitHub Enterprise.
func New(baseURL, token string) *Client {
	return &Client{api: rest.New(APIBase(baseURL), "Bearer "+token)}
}

// APIBase converte a URL web na URL da API.
func APIBase(baseURL string) string {
	baseURL = strings.TrimSuffix(baseURL, "/")
	u, err := url.Parse(baseURL)
	if err == nil && strings.EqualFold(u.Host, "github.com") {
		return "https://api.github.com"
	}
	return baseURL + "/api/v3"
}

type apiRepo struct {
	Owner struct {
		Login string `json:"login"`
	} `json:"owner"`
	Name        string `json:"name"`
	Description string `json:"description"`
	CloneURL    string `json:"clone_url"`
	HTMLURL     string `json:"html_url"`
	Private     bool   `json:"private"`
	Fork        bool   `json:"fork"`
	Permissions struct {
		Admin bool `json:"admin"`
	} `json:"permissions"`
}

func (r apiRepo) toForge() forge.Repo {
	return forge.Repo{
		Owner: r.Owner.Login, Name: r.Name, Description: r.Description,
		CloneURL: r.CloneURL, HTMLURL: r.HTMLURL, Private: r.Private, Fork: r.Fork,
		Admin: r.Permissions.Admin,
	}
}

func repoPath(owner, name string) string {
	return "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(name)
}

func (c *Client) Whoami(ctx context.Context) (string, error) {
	c.userOnce.Do(func() {
		var u struct {
			Login string `json:"login"`
		}
		c.userErr = c.api.Do(ctx, http.MethodGet, "/user", nil, &u)
		c.user = u.Login
	})
	return c.user, c.userErr
}

func (c *Client) ListRepos(ctx context.Context) ([]forge.Repo, error) {
	var out []forge.Repo
	for page := 1; ; page++ {
		var batch []apiRepo
		path := fmt.Sprintf("/user/repos?per_page=%d&page=%d", pageSize, page)
		if err := c.api.Do(ctx, http.MethodGet, path, nil, &batch); err != nil {
			return nil, err
		}
		for _, r := range batch {
			out = append(out, r.toForge())
		}
		if len(batch) < pageSize {
			return out, nil
		}
	}
}

// GetRepo também detecta se o repo está vazio (GitHub não expõe esse campo):
// um repo sem nenhuma branch é considerado vazio.
func (c *Client) GetRepo(ctx context.Context, owner, name string) (*forge.Repo, error) {
	var r apiRepo
	if err := c.api.Do(ctx, http.MethodGet, repoPath(owner, name), nil, &r); err != nil {
		return nil, err
	}
	fr := r.toForge()
	var branches []struct{}
	if err := c.api.Do(ctx, http.MethodGet, repoPath(owner, name)+"/branches?per_page=1", nil, &branches); err != nil {
		return nil, err
	}
	fr.Empty = len(branches) == 0
	return &fr, nil
}

func (c *Client) CreateRepo(ctx context.Context, opts forge.CreateRepoOpts) (*forge.Repo, error) {
	user, err := c.Whoami(ctx)
	if err != nil {
		return nil, err
	}
	path := "/user/repos"
	if opts.Owner != "" && !strings.EqualFold(opts.Owner, user) {
		path = "/orgs/" + url.PathEscape(opts.Owner) + "/repos"
	}
	body := map[string]any{
		"name":        opts.Name,
		"description": opts.Description,
		"private":     opts.Private,
		"auto_init":   false,
	}
	var r apiRepo
	if err := c.api.Do(ctx, http.MethodPost, path, body, &r); err != nil {
		return nil, err
	}
	fr := r.toForge()
	fr.Empty = true
	return &fr, nil
}

func (c *Client) ListOrgs(ctx context.Context) ([]string, error) {
	var out []string
	for page := 1; ; page++ {
		var batch []struct {
			Login string `json:"login"`
		}
		path := fmt.Sprintf("/user/orgs?per_page=%d&page=%d", pageSize, page)
		if err := c.api.Do(ctx, http.MethodGet, path, nil, &batch); err != nil {
			return nil, err
		}
		for _, o := range batch {
			out = append(out, o.Login)
		}
		if len(batch) < pageSize {
			return out, nil
		}
	}
}

func (c *Client) DeleteRepo(ctx context.Context, owner, name string) error {
	return c.api.Do(ctx, http.MethodDelete, repoPath(owner, name), nil, nil)
}
