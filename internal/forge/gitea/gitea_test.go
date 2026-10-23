package gitea

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vinicgobbi/git-sync/internal/forge"
)

func TestClient(t *testing.T) {
	var gotMirror map[string]any
	var gotCreatePath string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/user", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "token tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Write([]byte(`{"login":"vini"}`))
	})
	mux.HandleFunc("GET /api/v1/repos/vini/missing", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	mux.HandleFunc("POST /api/v1/orgs/acme/repos", func(w http.ResponseWriter, r *http.Request) {
		gotCreatePath = r.URL.Path
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"owner":{"login":"acme"},"name":"x","clone_url":"https://g/acme/x.git","empty":true}`))
	})
	mux.HandleFunc("GET /api/v1/repos/vini/app/push_mirrors", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"remote_name":"remote_mirror_1","remote_address":"https://github.com/vini/app.git",
			"interval":"8h0m0s","sync_on_commit":true,"created":"2024-01-02T03:04:05Z","last_update":"","last_error":""}]`))
	})
	mux.HandleFunc("POST /api/v1/repos/vini/app/push_mirrors", func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotMirror)
		w.Write([]byte(`{"remote_name":"remote_mirror_2"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx := context.Background()
	c := New(srv.URL+"/", "tok")

	if u, err := c.Whoami(ctx); err != nil || u != "vini" {
		t.Fatalf("Whoami = %q, %v", u, err)
	}
	if _, err := c.GetRepo(ctx, "vini", "missing"); !errors.Is(err, forge.ErrNotFound) {
		t.Fatalf("GetRepo missing: err = %v, want ErrNotFound", err)
	}
	repo, err := c.CreateRepo(ctx, forge.CreateRepoOpts{Owner: "acme", Name: "x"})
	if err != nil || gotCreatePath != "/api/v1/orgs/acme/repos" || !repo.Empty {
		t.Fatalf("CreateRepo em org: path=%q repo=%+v err=%v", gotCreatePath, repo, err)
	}
	ms, err := c.ListPushMirrors(ctx, "vini", "app")
	if err != nil || len(ms) != 1 || ms[0].Created.Year() != 2024 || !ms[0].LastUpdate.IsZero() {
		t.Fatalf("ListPushMirrors = %+v, %v", ms, err)
	}
	_, err = c.AddPushMirror(ctx, "vini", "app", forge.AddMirrorOpts{
		RemoteAddress: "https://codeberg.org/vini/app.git", Username: "vini", Password: "p", Interval: "1h0m0s", SyncOnCommit: true,
	})
	if err != nil || gotMirror["remote_password"] != "p" || gotMirror["sync_on_commit"] != true {
		t.Fatalf("AddPushMirror body=%v err=%v", gotMirror, err)
	}
}
