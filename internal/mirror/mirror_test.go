package mirror

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/vinicgobbi/git-sync/internal/config"
	"github.com/vinicgobbi/git-sync/internal/forge"
)

// fakeForge simula uma instância em memória (Gitea/Forgejo ou GitHub).
type fakeForge struct {
	mu        sync.Mutex
	user      string
	baseURL   string
	repos     map[string]*forge.Repo
	mirrors   map[string][]forge.PushMirror
	failAdd   bool
	deleted   []string
	mirrorsOn bool
	added     []forge.AddMirrorOpts // credenciais recebidas em AddPushMirror
}

func newFake(user, baseURL string) *fakeForge {
	return &fakeForge{user: user, baseURL: baseURL, repos: map[string]*forge.Repo{},
		mirrors: map[string][]forge.PushMirror{}, mirrorsOn: true}
}

func (f *fakeForge) add(r forge.Repo) { f.repos[r.FullName()] = &r }

func (f *fakeForge) Whoami(context.Context) (string, error) { return f.user, nil }
func (f *fakeForge) ListRepos(context.Context) ([]forge.Repo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []forge.Repo
	for _, r := range f.repos {
		out = append(out, *r)
	}
	return out, nil
}
func (f *fakeForge) GetRepo(_ context.Context, o, n string) (*forge.Repo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.repos[o+"/"+n]
	if !ok {
		return nil, forge.ErrNotFound
	}
	c := *r
	return &c, nil
}
func (f *fakeForge) CreateRepo(_ context.Context, o forge.CreateRepoOpts) (*forge.Repo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	owner := o.Owner
	if owner == "" {
		owner = f.user
	}
	r := &forge.Repo{Owner: owner, Name: o.Name, Private: o.Private, Empty: true, Admin: true,
		CloneURL: f.baseURL + "/" + owner + "/" + o.Name + ".git"}
	f.repos[r.FullName()] = r
	return r, nil
}
func (f *fakeForge) DeleteRepo(_ context.Context, o, n string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.repos, o+"/"+n)
	f.deleted = append(f.deleted, o+"/"+n)
	return nil
}
func (f *fakeForge) ListOrgs(context.Context) ([]string, error)   { return nil, nil }
func (f *fakeForge) MirrorsEnabled(context.Context) (bool, error) { return f.mirrorsOn, nil }
func (f *fakeForge) ListPushMirrors(_ context.Context, o, n string) ([]forge.PushMirror, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.mirrors[o+"/"+n], nil
}
func (f *fakeForge) AddPushMirror(_ context.Context, o, n string, opts forge.AddMirrorOpts) (*forge.PushMirror, error) {
	if f.failAdd {
		return nil, errors.New("boom")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.added = append(f.added, opts)
	m := forge.PushMirror{RemoteName: fmt.Sprintf("m%d", len(f.added)), RemoteAddress: opts.RemoteAddress,
		Interval: opts.Interval, SyncOnCommit: opts.SyncOnCommit}
	f.mirrors[o+"/"+n] = append(f.mirrors[o+"/"+n], m)
	return &m, nil
}
func (f *fakeForge) SyncPushMirrors(context.Context, string, string) error { return nil }
func (f *fakeForge) DeletePushMirror(_ context.Context, o, n, remote string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	ms := f.mirrors[o+"/"+n]
	for i, m := range ms {
		if m.RemoteName == remote {
			f.mirrors[o+"/"+n] = append(ms[:i:i], ms[i+1:]...)
			return nil
		}
	}
	return forge.ErrNotFound
}

var (
	srcIn = config.Instance{Name: "casa", Kind: config.KindForgejo, URL: "https://git.casa.lan"}
	ghIn  = config.Instance{Name: "github", Kind: config.KindGitHub, URL: "https://github.com"}
	cbIn  = config.Instance{Name: "codeberg", Kind: config.KindForgejo, URL: "https://codeberg.org"}
)

func setup() (*Request, *fakeForge, *fakeForge, *fakeForge) {
	src := newFake("vini", srcIn.URL)
	gh := newFake("vini-gh", "https://github.com")
	cb := newFake("vini", "https://codeberg.org")
	for _, n := range []string{"app", "lib", "notes"} {
		src.add(forge.Repo{Owner: "vini", Name: n, Admin: true, HTMLURL: srcIn.URL + "/vini/" + n})
	}
	repos, _ := src.ListRepos(context.Background())
	req := &Request{
		Source: srcIn, SourceForge: src, Repos: repos, Interval: "8h0m0s",
		Targets: []Target{
			{Instance: ghIn, Forge: gh, Username: "vini-gh", Password: "ghp"},
			{Instance: cbIn, Forge: cb, Username: "vini", Password: "cbp"},
		},
	}
	return req, src, gh, cb
}

func TestPlanExecuteIsIdempotent(t *testing.T) {
	ctx := context.Background()
	req, src, gh, cb := setup()
	// "lib" já existe vazio no GitHub; "notes" já existe com conteúdo no Codeberg.
	gh.add(forge.Repo{Owner: "vini-gh", Name: "lib", Empty: true, CloneURL: "https://github.com/vini-gh/lib.git"})
	cb.add(forge.Repo{Owner: "vini", Name: "notes", Empty: false})

	plan, err := BuildPlan(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if got := [5]int{plan.Count(ActionCreate), plan.Count(ActionReuse), plan.Count(ActionSkip),
		plan.Count(ActionConflict), plan.Count(ActionInvalid)}; got != [5]int{4, 1, 0, 1, 0} {
		t.Fatalf("contagem do plano = %v, want [4 1 0 1 0]", got)
	}

	sum := Execute(ctx, req, plan, ExecOptions{}, nil)
	if sum != (Summary{Done: 5, Skipped: 1}) {
		t.Fatalf("summary = %+v", sum)
	}
	if len(src.mirrors["vini/app"]) != 2 {
		t.Fatalf("vini/app deveria ter 2 mirrors: %+v", src.mirrors["vini/app"])
	}

	// Rodar de novo: tudo deve ser pulado (exceto o conflito, que continua conflito).
	plan2, err := BuildPlan(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if plan2.Count(ActionSkip) != 5 || plan2.Count(ActionConflict) != 1 {
		t.Fatalf("segundo plano não é idempotente: %+v", plan2.Steps)
	}
}

func TestForceReusesNonEmpty(t *testing.T) {
	req, _, _, cb := setup()
	cb.add(forge.Repo{Owner: "vini", Name: "notes", Empty: false})
	req.Force = true
	plan, err := BuildPlan(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Count(ActionConflict) != 0 || plan.Count(ActionReuse) != 1 {
		t.Fatalf("force deveria virar reuse: %+v", plan.Steps)
	}
}

func TestRollbackOnMirrorFailure(t *testing.T) {
	ctx := context.Background()
	req, src, gh, _ := setup()
	req.Targets = req.Targets[:1]
	src.failAdd = true
	plan, _ := BuildPlan(ctx, req)
	sum := Execute(ctx, req, plan, ExecOptions{Rollback: true}, nil)
	if sum.Failed != 3 || len(gh.deleted) != 3 || len(gh.repos) != 0 {
		t.Fatalf("rollback: sum=%+v deleted=%v repos=%v", sum, gh.deleted, gh.repos)
	}
}

func TestMirrorsDisabled(t *testing.T) {
	req, src, _, _ := setup()
	src.mirrorsOn = false
	if _, err := BuildPlan(context.Background(), req); err == nil {
		t.Fatal("esperava erro com espelhamento desabilitado")
	}
}

func TestDiscover(t *testing.T) {
	ctx := context.Background()
	req, src, _, _ := setup()
	plan, _ := BuildPlan(ctx, req)
	Execute(ctx, req, plan, ExecOptions{}, nil)
	src.mirrors["vini/app"] = append(src.mirrors["vini/app"], forge.PushMirror{RemoteAddress: "https://gitlab.com/x/app.git"})

	res := Discover(ctx, []Source{{srcIn, src}}, []config.Instance{srcIn, ghIn, cbIn}, 2)
	if len(res.Errors) != 0 || len(res.Links) != 7 {
		t.Fatalf("links=%d errors=%v", len(res.Links), res.Errors)
	}
	external := 0
	for _, l := range res.Links {
		if l.Target == nil {
			external++
		}
	}
	if external != 1 {
		t.Fatalf("esperava 1 destino externo, got %d", external)
	}
}

func TestNormalizeURL(t *testing.T) {
	cases := map[string]string{
		"https://user:tok@GitHub.com/Vini/App.git/": "github.com/vini/app",
		"http://git.lan/a/b":                        "git.lan/a/b",
		"https://git.lan/a/b@c":                     "git.lan/a/b@c",
	}
	for in, want := range cases {
		if got := forge.NormalizeURL(in); got != want {
			t.Errorf("NormalizeURL(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestRenew: os espelhos que enviam para um destino são recriados com a
// credencial nova, mantendo endereço, intervalo e envio a cada push; os
// demais não mudam, e uma falha deixa o espelho antigo no lugar.
func TestRenew(t *testing.T) {
	ctx := context.Background()
	req, src, _, _ := setup()
	req.SyncOnCommit = true
	plan, err := BuildPlan(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if sum := Execute(ctx, req, plan, ExecOptions{}, nil); sum.Failed != 0 {
		t.Fatalf("execução: %+v", sum)
	}
	res := Discover(ctx, []Source{{Instance: srcIn, Forge: src}}, []config.Instance{srcIn, ghIn, cbIn}, 2)
	var toGitHub []Link
	for _, l := range res.Links {
		if l.Target != nil && l.Target.Name == "github" {
			toGitHub = append(toGitHub, l)
		}
	}
	if len(toGitHub) != 3 {
		t.Fatalf("esperava 3 espelhos para o GitHub, há %d", len(toGitHub))
	}

	before := len(src.added)
	forges := map[string]forge.Mirrorer{"casa": src}
	n, errs := Renew(ctx, forges, toGitHub, "vini-gh", "novo-token", 2)
	if n != 3 || len(errs) != 0 {
		t.Fatalf("Renew = %d, %v", n, errs)
	}
	for _, opts := range src.added[before:] {
		if opts.Password != "novo-token" || opts.Username != "vini-gh" || opts.Interval != "8h0m0s" || !opts.SyncOnCommit {
			t.Fatalf("espelho recriado com %+v", opts)
		}
	}
	after := Discover(ctx, []Source{{Instance: srcIn, Forge: src}}, []config.Instance{srcIn, ghIn, cbIn}, 2)
	if len(after.Links) != len(res.Links) {
		t.Fatalf("o total de espelhos deveria continuar %d, ficou %d", len(res.Links), len(after.Links))
	}
	for _, l := range after.Links {
		if l.Target.Name == "github" && l.Mirror.RemoteName == toGitHub[0].Mirror.RemoteName {
			t.Fatalf("o espelho antigo %s deveria ter sido apagado", l.Mirror.RemoteName)
		}
	}

	// se a criação falha, o antigo fica
	src.failAdd = true
	again := Discover(ctx, []Source{{Instance: srcIn, Forge: src}}, []config.Instance{srcIn, ghIn, cbIn}, 2)
	if n, errs := Renew(ctx, forges, again.Links[:1], "u", "p", 1); n != 0 || len(errs) != 1 {
		t.Fatalf("com falha: %d, %v", n, errs)
	}
	if got := Discover(ctx, []Source{{Instance: srcIn, Forge: src}}, []config.Instance{srcIn, ghIn, cbIn}, 2); len(got.Links) != len(again.Links) {
		t.Fatal("uma falha ao recriar não pode apagar o espelho antigo")
	}
}
