// Package gitea implementa forge.Mirrorer para Gitea e Forgejo (API v1 compatível).
package gitea

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

const pageSize = 50

type Client struct {
	api *rest.Client

	userOnce sync.Once
	user     string
	userErr  error
}

var _ forge.Mirrorer = (*Client)(nil)

// New cria um cliente para a instância em baseURL (ex.: https://git.exemplo.com).
func New(baseURL, token string) *Client {
	return &Client{api: rest.New(strings.TrimSuffix(baseURL, "/")+"/api/v1", "token "+token)}
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
	Empty       bool   `json:"empty"`
	Mirror      bool   `json:"mirror"`
	Fork        bool   `json:"fork"`
	Permissions struct {
		Admin bool `json:"admin"`
	} `json:"permissions"`
}

func (r apiRepo) toForge() forge.Repo {
	return forge.Repo{
		Owner: r.Owner.Login, Name: r.Name, Description: r.Description,
		CloneURL: r.CloneURL, HTMLURL: r.HTMLURL,
		Private: r.Private, Empty: r.Empty, Mirror: r.Mirror, Fork: r.Fork,
		Admin: r.Permissions.Admin,
	}
}

type apiPushMirror struct {
	RemoteName    string         `json:"remote_name"`
	RemoteAddress string         `json:"remote_address"`
	Interval      string         `json:"interval"`
	SyncOnCommit  bool           `json:"sync_on_commit"`
	Created       forge.FlexTime `json:"created"`
	LastUpdate    forge.FlexTime `json:"last_update"`
	LastError     string         `json:"last_error"`
}

func (m apiPushMirror) toForge() forge.PushMirror {
	return forge.PushMirror{
		RemoteName: m.RemoteName, RemoteAddress: m.RemoteAddress,
		Interval: m.Interval, SyncOnCommit: m.SyncOnCommit,
		Created: m.Created.Time, LastUpdate: m.LastUpdate.Time, LastError: m.LastError,
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
		path := fmt.Sprintf("/user/repos?page=%d&limit=%d", page, pageSize)
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

func (c *Client) GetRepo(ctx context.Context, owner, name string) (*forge.Repo, error) {
	var r apiRepo
	if err := c.api.Do(ctx, http.MethodGet, repoPath(owner, name), nil, &r); err != nil {
		return nil, err
	}
	fr := r.toForge()
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
	return &fr, nil
}

func (c *Client) DeleteRepo(ctx context.Context, owner, name string) error {
	return c.api.Do(ctx, http.MethodDelete, repoPath(owner, name), nil, nil)
}

func (c *Client) MirrorsEnabled(ctx context.Context) (bool, error) {
	var s struct {
		MirrorsDisabled bool `json:"mirrors_disabled"`
	}
	if err := c.api.Do(ctx, http.MethodGet, "/settings/repository", nil, &s); err != nil {
		return false, err
	}
	return !s.MirrorsDisabled, nil
}

func (c *Client) ListPushMirrors(ctx context.Context, owner, repo string) ([]forge.PushMirror, error) {
	var out []forge.PushMirror
	for page := 1; ; page++ {
		var batch []apiPushMirror
		path := fmt.Sprintf("%s/push_mirrors?page=%d&limit=%d", repoPath(owner, repo), page, pageSize)
		if err := c.api.Do(ctx, http.MethodGet, path, nil, &batch); err != nil {
			return nil, err
		}
		for _, m := range batch {
			out = append(out, m.toForge())
		}
		if len(batch) < pageSize {
			return out, nil
		}
	}
}

func (c *Client) AddPushMirror(ctx context.Context, owner, repo string, opts forge.AddMirrorOpts) (*forge.PushMirror, error) {
	body := map[string]any{
		"remote_address":  opts.RemoteAddress,
		"remote_username": opts.Username,
		"remote_password": opts.Password,
		"interval":        opts.Interval,
		"sync_on_commit":  opts.SyncOnCommit,
	}
	var m apiPushMirror
	if err := c.api.Do(ctx, http.MethodPost, repoPath(owner, repo)+"/push_mirrors", body, &m); err != nil {
		return nil, err
	}
	fm := m.toForge()
	return &fm, nil
}

func (c *Client) SyncPushMirrors(ctx context.Context, owner, repo string) error {
	return c.api.Do(ctx, http.MethodPost, repoPath(owner, repo)+"/push_mirrors-sync", nil, nil)
}

func (c *Client) DeletePushMirror(ctx context.Context, owner, repo, remoteName string) error {
	path := repoPath(owner, repo) + "/push_mirrors/" + url.PathEscape(remoteName)
	return c.api.Do(ctx, http.MethodDelete, path, nil, nil)
}
