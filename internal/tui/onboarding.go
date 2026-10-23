package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type obStep int

const (
	obWelcome obStep = iota
	obDone
)

// onboarding guia a primeira configuração: explica o conceito, abre o
// cadastro da primeira instância e depois resume o que falta.
type onboarding struct {
	sh   *shared
	step obStep
}

var (
	okStart  = bind([]string{"enter"}, "enter", "começar")
	okSkip   = bind([]string{"esc"}, "esc", "pular")
	okManual = bind([]string{"a"}, "a", "adicionar instância")
	okFinish = bind([]string{"enter"}, "enter", "concluir")
)

func newOnboarding(sh *shared) *onboarding { return &onboarding{sh: sh} }

func (o *onboarding) Title() string   { return "Primeiros passos" }
func (o *onboarding) Capturing() bool { return false }
func (o *onboarding) Init() tea.Cmd   { return nil }

func (o *onboarding) Keys() []key.Binding {
	if o.step == obWelcome {
		return []key.Binding{okStart, okSkip}
	}
	return []key.Binding{okFinish, okManual}
}

func (o *onboarding) Update(msg tea.Msg) (view, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return o, nil
	}
	switch o.step {
	case obWelcome:
		switch k.String() {
		case "enter":
			// o resumo aparece quando o formulário fechar
			o.step = obDone
			return o, open(newInstanceForm(o.sh, nil))
		case "esc":
			return o, closeFlow()
		}
	case obDone:
		switch {
		case key.Matches(k, okManual):
			return o, open(newInstanceForm(o.sh, nil))
		case k.String() == "enter", k.String() == "esc":
			text := `Tudo pronto! Crie seu primeiro espelho em "Novo espelho".`
			if len(o.sh.env.Cfg.Sources()) == 0 {
				text = "Cadastre uma instância Gitea ou Forgejo para começar a espelhar."
			}
			return o, tea.Batch(closeFlow(), notify(toastInfo, text))
		}
	}
	return o, nil
}

func (o *onboarding) View(w, h int) string {
	cw := min(w-4, 84)
	body := o.viewWelcome(cw)
	if o.step == obDone {
		body = o.viewDone(cw)
	}
	return lipgloss.NewStyle().Width(cw).Render(body)
}

func (o *onboarding) viewWelcome(w int) string {
	box := func(title, sub string) string {
		return sCard.Padding(0, 2).Align(lipgloss.Center).Render(sBold.Render(title) + "\n" + sMuted.Render(sub))
	}
	arrow := lipgloss.NewStyle().Foreground(colAccent).Render("\n──── push mirror ───▶")
	diagram := lipgloss.JoinHorizontal(lipgloss.Center,
		box("Gitea · Forgejo", "origem"), " ", arrow, " ", box("GitHub · Gitea · Forgejo", "destinos"))

	text := "O git-sync usa o espelhamento nativo do Gitea e do Forgejo: ele cria o " +
		"repositório vazio no destino e configura a origem para enviar cada push automaticamente. " +
		"Depois disso, nada precisa ficar rodando na sua máquina."
	steps := []string{
		sAccent.Render("1.") + " Cadastrar suas contas (a origem e os destinos), cada uma com um token de acesso",
		sAccent.Render("2.") + " Escolher repositórios e para onde enviá-los",
		sAccent.Render("3.") + " Acompanhar os envios no painel",
	}
	tokens := "Toda a autenticação é feita por tokens que você cria em cada conta. Ao cadastrar, o git-sync " +
		"mostra onde criá-los e quais permissões dar. Eles ficam no keyring do sistema."
	return "\n" + sTitle.Render("Bem-vindo ao git-sync") + "\n\n" + wrap(text, w) + "\n\n" +
		lipgloss.PlaceHorizontal(w, lipgloss.Center, diagram) + "\n\n" + strings.Join(steps, "\n") + "\n\n" +
		wrap(tokens, w) + "\n\n" + keyHint("enter", "cadastrar a primeira conta") + "   " + keyHint("esc", "pular")
}

func (o *onboarding) viewDone(w int) string {
	cfg := o.sh.env.Cfg
	var srcs, dsts []string
	for _, in := range cfg.Instances {
		if in.CanBeSource() {
			srcs = append(srcs, in.Name)
		}
		dsts = append(dsts, in.Name)
	}
	var b strings.Builder
	b.WriteString("\n" + sTitle.Render("Resumo") + "\n\n")
	b.WriteString(sLabel.Width(20).Render("Podem ser origem") + listOrNone(srcs) + "\n")
	b.WriteString(sLabel.Width(20).Render("Podem ser destino") + listOrNone(dsts) + "\n\n")
	switch {
	case len(srcs) == 0:
		b.WriteString(sWarn.Render("⚠ ") + wrap("Ainda falta uma origem: cadastre a instância Gitea ou Forgejo onde estão seus repositórios.", w-2) + "\n")
	case len(cfg.Instances) < 2:
		b.WriteString(sWarn.Render("⚠ ") + wrap("Cadastre também a instância de destino (por exemplo, o GitHub).", w-2) + "\n")
	default:
		b.WriteString(sOK.Render("✔ ") + "Tudo pronto para criar espelhos.\n")
	}
	b.WriteString(fmt.Sprintf("\n%s   %s", keyHint("enter", "ir para o painel"), keyHint("a", "adicionar outra instância")))
	return b.String()
}

func listOrNone(xs []string) string {
	if len(xs) == 0 {
		return sMuted.Render("nenhuma")
	}
	return strings.Join(xs, ", ")
}
