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
	"github.com/charmbracelet/lipgloss"

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

func (d *driver) mirrors() *mirrorsView { return d.m.mirrors }

// menu volta ao menu, vai até o item e abre com enter.
func (d *driver) menu(label string) {
	d.t.Helper()
	if d.m.panel {
		d.key("esc")
	}
	d.key("g")
	for range d.m.refs {
		if d.m.item().label == label {
			d.key("enter")
			return
		}
		d.key("down")
	}
	d.t.Fatalf("item %q não está no menu:\n%s", label, d.view())
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

	d := newDriver(t, env)
	d.mustSee("Nenhum espelho ainda")

	// cadastra o Forgejo pelo formulário (tipo detectado automaticamente)
	d.menu("Adicionar instância")
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
	d.menu("Novo espelho")
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
	d.menu("Espelhos")
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
	if !d.m.panel {
		d.menu("Espelhos") // um esc a mais devolve o foco ao menu
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
	for _, w := range []int{60, 90, 200} { // nada quebra nem estoura em larguras diferentes
		d.send(tea.WindowSizeMsg{Width: w, Height: 30})
		d.key("g")
		for range d.m.refs {
			for i, l := range strings.Split(d.view(), "\n") {
				if lw := lipgloss.Width(l); lw > w {
					t.Fatalf("largura %d, item %q: linha %d tem %d colunas:\n%s", w, d.m.item().label, i, lw, d.view())
				}
			}
			d.key("down")
		}
	}
}

// TestMenuFocus: enter leva o foco do menu ao painel e esc devolve; um
// item de ação mostra a descrição e o comando equivalente.
func TestMenuFocus(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	env := &app.Env{CfgPath: filepath.Join(t.TempDir(), "c.yaml"), Cfg: config.New(), Store: secrets.Memory{}}
	d := newDriver(t, env)
	d.key("esc", "esc") // fecha o onboarding da primeira vez
	d.mustSee("ESPELHOS", "▸ Espelhos", "Nenhuma conta ainda")

	d.key("enter")
	if !d.m.panel {
		t.Fatalf("enter num item de tela deveria focar o painel:\n%s", d.view())
	}
	d.mustSee("● Espelhos", "esc menu")
	d.key("esc")
	if d.m.panel {
		t.Fatalf("esc deveria devolver o foco ao menu")
	}

	d.key("down")
	d.mustSee("▸ Novo espelho", "Pelo terminal:", "git-sync mirror")
	d.key("enter") // sem instâncias: avisa em vez de abrir
	if len(d.m.flows) != 0 {
		t.Fatalf("novo espelho sem instâncias não deveria abrir o assistente")
	}
	d.mustSee("Adicionar instância")

	d.menu("Adicionar instância")
	if len(d.m.flows) != 1 {
		t.Fatalf("Adicionar instância deveria abrir o formulário:\n%s", d.view())
	}
}

// TestInstallItem: "Instalar no PATH" copia o binário, põe a pasta no PATH
// pelo arquivo do shell e some do menu.
func TestInstallItem(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/zsh")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	env := &app.Env{CfgPath: filepath.Join(t.TempDir(), "c.yaml"), Cfg: config.New(), Store: secrets.Memory{}}
	d := newDriver(t, env)
	d.key("esc", "esc")

	d.menu("Instalar no PATH")
	d.mustSee("Instalar o git-sync?", filepath.Join(home, ".zshrc"))
	d.key("right", "enter")
	dst := filepath.Join(home, ".local", "bin", "git-sync")
	if _, err := os.Stat(dst); err != nil {
		t.Fatalf("binário não instalado: %v\n%s", err, d.view())
	}
	if rc, _ := os.ReadFile(filepath.Join(home, ".zshrc")); !strings.Contains(string(rc), `export PATH="$HOME/.local/bin:$PATH"`) {
		t.Fatalf(".zshrc sem o PATH:\n%s", rc)
	}
	d.mustSee("git-sync instalado em " + dst)
	if strings.Contains(d.view(), "Instalar no PATH") {
		t.Fatalf("o item deveria sumir depois de instalar:\n%s", d.view())
	}
}

// TestInstallItemHiddenWhenInstalled: com este binário já no PATH, a opção
// nem aparece.
func TestInstallItemHiddenWhenInstalled(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	b, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "git-sync"), b, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("PATH", bin)
	env := &app.Env{CfgPath: filepath.Join(t.TempDir(), "c.yaml"), Cfg: config.New(), Store: secrets.Memory{}}
	d := newDriver(t, env)
	d.key("esc", "esc")
	d.mustSee("ESTA FERRAMENTA", "Diagnóstico")
	if strings.Contains(d.view(), "Instalar no PATH") {
		t.Fatalf("já instalado: a opção não deveria aparecer:\n%s", d.view())
	}
}
