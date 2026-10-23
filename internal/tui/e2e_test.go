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

func newDriver(t *testing.T, env *app.Env) *driver {
	tick = func(time.Duration, func(time.Time) tea.Msg) tea.Cmd { return nil }
	t.Cleanup(func() { tick = tea.Tick })
	d := &driver{t: t, m: newModel(context.Background(), env, "test")}
	d.send(tea.WindowSizeMsg{Width: 150, Height: 45})
	d.run(d.m.Init())
	return d
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

var namedKeys = map[string]tea.KeyType{
	"enter": tea.KeyEnter, "esc": tea.KeyEsc, "up": tea.KeyUp, "down": tea.KeyDown,
	"left": tea.KeyLeft, "right": tea.KeyRight, "tab": tea.KeyTab,
	"ctrl+a": tea.KeyCtrlA, "ctrl+u": tea.KeyCtrlU, "ctrl+s": tea.KeyCtrlS,
}

func (d *driver) key(keys ...string) {
	for _, k := range keys {
		switch {
		case k == "space":
			d.send(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")})
		case namedKeys[k] != 0:
			d.send(tea.KeyMsg{Type: namedKeys[k]})
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

func (d *driver) mustSee(want ...string) {
	d.t.Helper()
	v := d.view()
	for _, w := range want {
		if !strings.Contains(v, w) {
			d.t.Fatalf("esperava %q na tela:\n%s", w, v)
		}
	}
}

func (d *driver) mirrors() *mirrorsView { return d.m.tabs[0].(*mirrorsView) }

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

	d := newDriver(t, env)
	d.mustSee("Nenhum espelho ainda")

	// cadastra o Forgejo pelo formulário (tipo detectado automaticamente)
	d.key("2", "a")
	d.mustSee("Adicionar instância", "Gitea e Forgejo podem ser origem")
	d.key("tab")
	d.typeText(fURL)
	d.key("tab", "ctrl+u")
	d.typeText("fj")
	d.key("tab", "tab") // credencial (token) → campo do token
	d.typeText(fTok)
	d.key("ctrl+s")
	d.mustSee("fj conectada como vini")
	if in, ok := env.Cfg.Find("fj"); !ok || in.Kind != config.KindForgejo || in.User != "vini" {
		t.Fatalf("instância fj = %+v", in)
	}

	// novo espelho pelo assistente
	d.key("1", "n")
	d.mustSee("● Origem", "casa")
	d.key("enter") // origem casa
	d.mustSee("Quais repositórios de casa?", "vini/app", "vini/lib")
	d.typeText("li")
	d.mustSee("1 de 2 visíveis")
	d.key("esc", "ctrl+a", "enter") // limpa filtro, marca todos
	d.mustSee("Para onde enviar 2 repositórios?")
	d.key("space")
	d.mustSee("conta", "vini")
	d.key("enter") // opções
	d.mustSee("Como os espelhos devem funcionar?")
	d.key("enter") // revisão
	d.mustSee("2 repositórios serão criados", "executar 2 espelhos")
	t.Log("\n" + d.view())
	d.key("enter")
	d.mustSee("2 espelhos configurados")
	t.Log("\n" + d.view())

	d.key("enter") // volta ao painel e recarrega
	if n := len(d.mirrors().links); n != 2 {
		t.Fatalf("painel deveria listar 2 espelhos, tem %d:\n%s", n, d.view())
	}

	// a cópia é assíncrona no servidor: espera o conteúdo chegar no Forgejo
	deadline := time.Now().Add(60 * time.Second)
	for {
		r, err := fjClean.GetRepo(ctx, "vini", "app")
		if err == nil && !r.Empty {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("conteúdo não chegou ao Forgejo: repo=%+v err=%v", r, err)
		}
		time.Sleep(2 * time.Second)
	}
	d.key("r")
	d.mustSee("2 em dia")
	t.Log("\n" + d.view())

	// rodar de novo é idempotente
	d.key("n", "enter", "ctrl+a", "enter", "space", "enter", "enter")
	d.mustSee("2 já espelhados", "Nada a fazer")
	d.key("esc", "esc", "esc", "esc", "esc")
	if len(d.m.flows) != 0 {
		t.Fatalf("esc deveria voltar até o painel:\n%s", d.view())
	}

	// remover pede confirmação; o botão inicial é Cancelar
	d.key("x")
	d.mustSee("Remover este espelho?")
	d.key("enter") // cancela
	if n := len(d.mirrors().links); n != 2 {
		t.Fatalf("enter no modal deveria cancelar")
	}
	d.key("x", "right", "enter")
	if n := len(d.mirrors().links); n != 1 {
		t.Fatalf("depois de remover deveria sobrar 1 espelho:\n%s", d.view())
	}
}

// TestOnboardingFirstRun: sem instâncias, a TUI abre direto no onboarding.
func TestOnboardingFirstRun(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // sem logins do tea
	t.Setenv("PATH", t.TempDir())            // sem gh/tea
	env := &app.Env{CfgPath: filepath.Join(t.TempDir(), "c.yaml"), Cfg: config.New(), Store: secrets.Memory{}}
	d := newDriver(t, env)
	d.mustSee("Primeiros passos", "Bem-vindo ao git-sync", "push mirror")
	d.key("enter")
	d.mustSee("Não encontramos logins")
	d.key("esc", "esc")
	d.mustSee("Bem-vindo ao git-sync", "configurar agora")
	for _, w := range []int{60, 90, 200} { // nada quebra em larguras diferentes
		d.send(tea.WindowSizeMsg{Width: w, Height: 30})
		for _, tab := range []string{"1", "2", "3"} {
			d.key(tab)
			_ = d.view()
		}
	}
}
