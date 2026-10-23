package app

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/vinicgobbi/git-sync/internal/config"
)

// DetectKind distingue Forgejo de Gitea: só o Forgejo expõe /api/forgejo/v1/version.
func DetectKind(ctx context.Context, baseURL string) config.Kind {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	u := strings.TrimSuffix(baseURL, "/") + "/api/forgejo/v1/version"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return config.KindGitea
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return config.KindGitea
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return config.KindForgejo
	}
	return config.KindGitea
}
