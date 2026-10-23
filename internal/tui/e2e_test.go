package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/vinicgobbi/git-sync/internal/app"
	"github.com/vinicgobbi/git-sync/internal/config"
	"github.com/vinicgobbi/git-sync/internal/forge/gitea"
	"github.com/vinicgobbi/git-sync/internal/secrets"
)

// driver executa o modelo da TUI de forma síncrona, como o tea.Program faria.
type driver struct {
	t *testing.T
	m model
}

func (d *driver) send(msg tea.Msg) {
	nm, cmd := d.m.Update(msg)
	d.m = nm.(model)
	d.run(cmd)
}

func (d *driver) run(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case nil, spinner.TickMsg, tea.QuitMsg:
	case tea.BatchMsg:
		for _, c := range msg {
			d.run(c)
		}
	default:
		d.send(msg)
	}
}

func (d *driver) key(keys ...string) {
	for _, k := range keys {
		switch k {
		case "enter":
			d.send(tea.KeyMsg{Type: tea.KeyEnter})
		case "esc":
			d.send(tea.KeyMsg{Type: tea.KeyEsc})
		case "down":
			d.send(tea.KeyMsg{Type: tea.KeyDown})
		case "tab":
			d.send(tea.KeyMsg{Type: tea.KeyTab})
		case "space":
			d.send(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")})
		default:
			d.send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
		}
	}
}

func (d *driver) typeText(s string) {
	for _, r := range s {
		d.send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func (d *driver) view() string { return d.m.View() }

func (d *driver) mustSee(want string) {
	d.t.Helper()
	if v := d.view(); !strings.Contains(v, want) {
		d.t.Fatalf("esperava %q na tela:\n%s", want, v)
	}
}

// TestE2E precisa de um Gitea e um Forgejo de teste:
//
//	GITSYNC_E2E_GITEA_URL, GITSYNC_E2E_GITEA_TOKEN (com repos vini/app e vini/lib)
//	GITSYNC_E2E_FORGEJO_URL, GITSYNC_E2E_FORGEJO_TOKEN
func TestE2E(t *testing.T) {
	gURL, gTok := os.Getenv("GITSYNC_E2E_GITEA_URL"), os.Getenv("GITSYNC_E2E_GITEA_TOKEN")
	fURL, fTok := os.Getenv("GITSYNC_E2E_FORGEJO_URL"), os.Getenv("GITSYNC_E2E_FORGEJO_TOKEN")
	if gURL == "" || fURL == "" {
		t.Skip("defina GITSYNC_E2E_* para rodar contra instâncias reais")
	}
	ctx := context.Background()
	env := &app.Env{CfgPath: filepath.Join(t.TempDir(), "config.yaml"), Cfg: config.New(), Store: secrets.Memory{}}
	if _, err := env.AddInstance(ctx, config.Instance{Name: "casa", URL: gURL}, gTok, ""); err != nil {
		t.Fatal(err)
	}

	// estado limpo: sem mirrors na origem e sem os repos no destino
	casa, _ := app.ConnectSource(config.Instance{Name: "casa", Kind: config.KindGitea, URL: gURL}, env.Store)
	fjClean := gitea.New(fURL, fTok)
	for _, n := range []string{"app", "lib"} {
		ms, _ := casa.ListPushMirrors(ctx, "vini", n)
		for _, m := range ms {
			_ = casa.DeletePushMirror(ctx, "vini", n, m.RemoteName)
		}
		_ = fjClean.DeleteRepo(ctx, "vini", n)
	}

	sh := &shared{ctx: ctx, env: env}
	d := &driver{t: t, m: model{sh: sh, stack: []screen{newDashboard(sh)}}}
	d.send(tea.WindowSizeMsg{Width: 140, Height: 40})
	d.run(d.m.Init())
	d.mustSee("Nenhum push mirror")

	// cadastra o Forgejo pela tela de instâncias (tipo detectado automaticamente)
	d.key("i", "a")
	d.typeText("fj")
	d.key("tab")
	d.typeText(fURL)
	d.key("tab", "tab")
	d.typeText(fTok)
	d.key("enter", "enter") // pula token de mirror e salva
	d.mustSee("forgejo")
	if in, ok := env.Cfg.Find("fj"); !ok || in.Kind != config.KindForgejo || in.User != "vini" {
		t.Fatalf("instância fj = %+v", in)
	}
	d.key("esc")

	// novo espelho: marca os dois repos, escolhe o destino fj e planeja
	d.key("n")
	d.mustSee("vini/app")
	d.key("a", "enter")
	d.mustSee("Destinos")
	d.key("enter", "down", "enter") // marca fj; desce para o owner; enter = planejar
	d.mustSee("2 criar")
	t.Log("\n" + d.view())
	d.key("enter")
	d.mustSee("concluído")
	d.mustSee("2 ok")
	t.Log("\n" + d.view())

	d.key("enter") // volta ao painel e recarrega
	dash := d.m.stack[0].(*dashboard)
	if len(dash.links) != 2 {
		t.Fatalf("painel deveria listar 2 mirrors, tem %d:\n%s", len(dash.links), d.view())
	}

	// a sincronização é assíncrona no servidor: espera o conteúdo chegar no Forgejo
	fj, _ := app.Connect(config.Instance{Name: "fj", Kind: config.KindForgejo, URL: fURL}, env.Store)
	deadline := time.Now().Add(60 * time.Second)
	for {
		r, err := fj.GetRepo(ctx, "vini", "app")
		if err == nil && !r.Empty {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("conteúdo não chegou ao Forgejo: repo=%+v err=%v", r, err)
		}
		time.Sleep(2 * time.Second)
	}
	d.key("r")
	t.Log("\n" + d.view())

	// rodar de novo é idempotente: nada a fazer
	d.key("n", "a", "enter", "enter", "down", "enter")
	d.mustSee("2 já existe")
	d.mustSee("nada a fazer")
	d.key("esc", "esc", "esc")

	// remove um mirror pelo painel
	d.key("x")
	d.mustSee("remover o mirror")
	d.key("y")
	if dash := d.m.stack[0].(*dashboard); len(dash.links) != 1 {
		t.Fatalf("depois de remover deveria sobrar 1 mirror:\n%s", d.view())
	}
}
