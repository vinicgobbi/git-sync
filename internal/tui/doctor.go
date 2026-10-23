package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/vinicgobbi/git-sync/internal/app"
	"github.com/vinicgobbi/git-sync/internal/config"
	"github.com/vinicgobbi/git-sync/internal/deps"
)

// doctor mostra dependências, status das instâncias e permite importar logins.
type doctor struct {
	sh       *shared
	spin     spinner.Model
	busy     string // mensagem enquanto algo roda
	checks   []deps.Check
	insts    []config.Instance
	statuses []app.InstanceStatus
	imported []app.ImportItem
	err      error
}

type doctorResultMsg struct {
	checks   []deps.Check
	insts    []config.Instance
	statuses []app.InstanceStatus
}

type importResultMsg struct {
	items []app.ImportItem
	err   error
}

func newDoctor(sh *shared) *doctor {
	return &doctor{sh: sh, spin: spinner.New(spinner.WithSpinner(spinner.Dot))}
}

func (d *doctor) Title() string { return "Doctor" }
func (d *doctor) Keys() string {
	return "m importar logins do tea/gh • r verificar de novo • esc voltar"
}

func (d *doctor) Init() tea.Cmd { return d.run() }

func (d *doctor) run() tea.Cmd {
	d.busy = "verificando…"
	sh := d.sh
	insts := append([]config.Instance(nil), sh.env.Cfg.Instances...)
	return tea.Batch(d.spin.Tick, func() tea.Msg {
		res := doctorResultMsg{checks: deps.Run(sh.ctx), insts: insts}
		for _, in := range insts {
			res.statuses = append(res.statuses, sh.env.CheckInstance(sh.ctx, in))
		}
		return res
	})
}

func (d *doctor) Update(msg tea.Msg) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if d.busy != "" {
			var cmd tea.Cmd
			d.spin, cmd = d.spin.Update(msg)
			return d, cmd
		}
	case doctorResultMsg:
		d.busy = ""
		d.checks, d.insts, d.statuses = msg.checks, msg.insts, msg.statuses
	case importResultMsg:
		d.busy = ""
		d.imported, d.err = msg.items, msg.err
		return d, d.run()
	case tea.KeyMsg:
		if d.busy != "" {
			return d, nil
		}
		switch msg.String() {
		case "esc", "q":
			return d, pop()
		case "r":
			return d, d.run()
		case "m":
			d.busy = "importando…"
			sh := d.sh
			return d, tea.Batch(d.spin.Tick, func() tea.Msg {
				items, err := sh.env.Import(sh.ctx, false)
				return importResultMsg{items, err}
			})
		}
	}
	return d, nil
}

func (d *doctor) View() string {
	var b strings.Builder
	b.WriteString("\n" + boldStyle.Render("Ferramentas") + "\n")
	for _, c := range d.checks {
		mark := okStyle.Render("✔")
		if !c.Found {
			mark = helpStyle.Render("–")
			if c.Required {
				mark = errStyle.Render("✘")
			}
		}
		fmt.Fprintf(&b, "  %s %-4s %s\n", mark, c.Name, helpStyle.Render(c.Version))
		if c.Detail != "" {
			fmt.Fprintf(&b, "         %s\n", c.Detail)
		}
	}

	b.WriteString("\n" + boldStyle.Render("Instâncias") + helpStyle.Render("  ("+d.sh.env.CfgPath+")") + "\n")
	if len(d.insts) == 0 && d.busy == "" {
		b.WriteString("  nenhuma — pressione m para importar do tea/gh, ou cadastre em Instâncias\n")
	}
	for i, in := range d.insts {
		if i >= len(d.statuses) {
			break
		}
		st := d.statuses[i]
		line := ""
		switch {
		case st.Err != nil:
			line = errStyle.Render("✘ " + truncate(oneLine(st.Err.Error()), d.sh.width-30))
		case st.MirrorsDisabled:
			line = errStyle.Render("✘ espelhamento desabilitado na instância")
		default:
			line = okStyle.Render("✔") + " logado como " + st.User
			if st.MirrorToken {
				line += helpStyle.Render(" · token de mirror dedicado")
			} else if in.Kind == config.KindGitHub {
				line += warnStyle.Render(" · sem token de mirror dedicado")
			}
		}
		fmt.Fprintf(&b, "  %-14s %-8s %s\n", in.Name, in.Kind, line)
	}

	if d.err != nil {
		b.WriteString("\n" + errStyle.Render("✘ "+d.err.Error()) + "\n")
	}
	if len(d.imported) > 0 {
		b.WriteString("\n" + boldStyle.Render("Importação") + "\n")
		for _, it := range d.imported {
			switch {
			case it.Skipped:
				fmt.Fprintf(&b, "  – %s (%s): já existia\n", it.Name, it.From)
			case it.Err != nil:
				fmt.Fprintf(&b, "  %s %s (%s): %s\n", errStyle.Render("✘"), it.Name, it.From, oneLine(it.Err.Error()))
			default:
				fmt.Fprintf(&b, "  %s %s (%s, via %s)\n", okStyle.Render("✔"), it.Name, it.Inst.Kind, it.From)
				if it.From == "gh" {
					b.WriteString(warnStyle.Render("    o token do gh é OAuth e ficaria gravado nos mirrors; prefira um fine-grained PAT\n"+
						"    (Contents + Workflows: write) como token de mirror em Instâncias → e") + "\n")
				}
			}
		}
	}
	if d.busy != "" {
		b.WriteString("\n" + d.spin.View() + " " + d.busy + "\n")
	}
	return b.String()
}
