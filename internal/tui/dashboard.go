package tui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/vinicgobbi/git-sync/internal/forge"
	"github.com/vinicgobbi/git-sync/internal/mirror"
)

// dashboard lista os push mirrors existentes em todas as origens.
type dashboard struct {
	sh      *shared
	table   table.Model
	spin    spinner.Model
	loading bool
	links   []mirror.Link
	sources map[string]forge.Mirrorer
	errs    []error
	status  string
	confirm *confirmation
}

type confirmation struct {
	prompt string
	run    tea.Cmd
}

type discoveredMsg struct {
	res     mirror.DiscoverResult
	sources map[string]forge.Mirrorer
	err     error
}

type dashActionMsg struct {
	msg    string
	err    error
	reload bool
}

var dashColumns = []table.Column{
	{Title: "Origem", Width: 12},
	{Title: "Repositório", Width: 28},
	{Title: "Destino", Width: 12},
	{Title: "Último sync", Width: 16},
	{Title: "Status", Width: 0},
}

func newDashboard(sh *shared) *dashboard {
	return &dashboard{sh: sh, table: newTable(dashColumns), spin: spinner.New(spinner.WithSpinner(spinner.Dot))}
}

func (d *dashboard) Title() string { return "Painel" }

func (d *dashboard) Keys() string {
	if d.confirm != nil {
		return "y confirmar • qualquer tecla cancela"
	}
	return "n novo espelho • s sincronizar • x remover • enter detalhes • r recarregar • i instâncias • d doctor • q sair"
}

func (d *dashboard) Init() tea.Cmd { return d.reload() }

func (d *dashboard) reload() tea.Cmd {
	if len(d.sh.env.Cfg.Sources()) == 0 {
		d.loading, d.links = false, nil
		setRows(&d.table, nil)
		return nil
	}
	d.loading = true
	sh := d.sh
	return tea.Batch(d.spin.Tick, func() tea.Msg {
		srcs, err := sh.env.Sources()
		if err != nil {
			return discoveredMsg{err: err}
		}
		byName := map[string]forge.Mirrorer{}
		for _, s := range srcs {
			byName[s.Instance.Name] = s.Forge
		}
		res := mirror.Discover(sh.ctx, srcs, sh.env.Cfg.Instances, sh.env.Cfg.Defaults.Concurrency)
		return discoveredMsg{res: res, sources: byName}
	})
}

func (d *dashboard) Update(msg tea.Msg) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		d.table.SetColumns(fitColumns(dashColumns, msg.Width))
		d.table.SetHeight(max(d.sh.bodyHeight()-3, 3))
	case refreshMsg:
		return d, d.reload()
	case spinner.TickMsg:
		if d.loading {
			var cmd tea.Cmd
			d.spin, cmd = d.spin.Update(msg)
			return d, cmd
		}
	case discoveredMsg:
		d.loading = false
		if msg.err != nil {
			d.errs = []error{msg.err}
			return d, nil
		}
		d.links, d.errs, d.sources = msg.res.Links, msg.res.Errors, msg.sources
		setRows(&d.table, d.rows())
	case dashActionMsg:
		d.status = msg.msg
		if msg.err != nil {
			d.status = errStyle.Render("✘ " + msg.err.Error())
		}
		if msg.reload {
			return d, d.reload()
		}
	case tea.KeyMsg:
		return d.handleKey(msg)
	}
	return d, nil
}

func (d *dashboard) handleKey(msg tea.KeyMsg) (screen, tea.Cmd) {
	if d.confirm != nil {
		c := d.confirm
		d.confirm = nil
		if msg.String() == "y" {
			return d, c.run
		}
		d.status = "cancelado"
		return d, nil
	}
	switch msg.String() {
	case "q":
		return d, tea.Quit
	case "r":
		return d, d.reload()
	case "i":
		return d, push(newInstancesScreen(d.sh))
	case "d":
		return d, push(newDoctor(d.sh))
	case "n":
		if len(d.sh.env.Cfg.Sources()) == 0 {
			d.status = warnStyle.Render("cadastre ao menos uma instância Gitea/Forgejo (i) ou importe do tea (d)")
			return d, nil
		}
		return d, push(newRepoPicker(d.sh))
	case "enter":
		if l, ok := d.selected(); ok {
			d.status = fmt.Sprintf("%s/%s → %s (intervalo %s, sync on commit %v)",
				l.Source.Name, l.Repo.FullName(), l.Mirror.RemoteAddress, l.Mirror.Interval, l.Mirror.SyncOnCommit)
			if l.Mirror.LastError != "" {
				d.status += "\n" + errStyle.Render(oneLine(l.Mirror.LastError))
			}
		}
		return d, nil
	case "s":
		if l, ok := d.selected(); ok {
			return d, d.syncCmd(l)
		}
	case "x":
		if l, ok := d.selected(); ok {
			d.confirm = &confirmation{
				prompt: fmt.Sprintf("remover o mirror %s de %s? (o repo de destino NÃO é apagado)", l.Mirror.RemoteAddress, l.Repo.FullName()),
				run:    d.deleteCmd(l),
			}
		}
		return d, nil
	}
	var cmd tea.Cmd
	d.table, cmd = d.table.Update(msg)
	return d, cmd
}

func (d *dashboard) selected() (mirror.Link, bool) {
	i := d.table.Cursor()
	if d.loading || i < 0 || i >= len(d.links) {
		return mirror.Link{}, false
	}
	return d.links[i], true
}

func (d *dashboard) syncCmd(l mirror.Link) tea.Cmd {
	src, ctx := d.sources[l.Source.Name], d.sh.ctx
	d.status = "sincronizando " + l.Repo.FullName() + "…"
	return func() tea.Msg {
		if err := src.SyncPushMirrors(ctx, l.Repo.Owner, l.Repo.Name); err != nil {
			return dashActionMsg{err: err}
		}
		return dashActionMsg{msg: okStyle.Render("✔ sincronização disparada para " + l.Repo.FullName() + " (pode levar alguns segundos; r para recarregar)")}
	}
}

func (d *dashboard) deleteCmd(l mirror.Link) tea.Cmd {
	src, ctx := d.sources[l.Source.Name], d.sh.ctx
	return func() tea.Msg {
		if err := src.DeletePushMirror(ctx, l.Repo.Owner, l.Repo.Name, l.Mirror.RemoteName); err != nil {
			return dashActionMsg{err: err}
		}
		return dashActionMsg{msg: okStyle.Render("✔ mirror removido"), reload: true}
	}
}

func (d *dashboard) rows() []table.Row {
	rows := make([]table.Row, len(d.links))
	for i, l := range d.links {
		dst := "externo"
		if l.Target != nil {
			dst = l.Target.Name
		}
		last, st := "nunca", "… aguardando 1º sync"
		if !l.Mirror.LastUpdate.IsZero() {
			last = l.Mirror.LastUpdate.Local().Format("2006-01-02 15:04")
			st = "✔ ok"
		}
		if l.Mirror.LastError != "" {
			st = "✘ " + oneLine(l.Mirror.LastError)
		}
		rows[i] = table.Row{l.Source.Name, l.Repo.FullName(), dst, last, st}
	}
	return rows
}

func (d *dashboard) View() string {
	cfg := d.sh.env.Cfg
	switch {
	case len(cfg.Instances) == 0:
		return "\nNenhuma instância configurada.\n\n" +
			"  " + boldStyle.Render("d") + "  doctor: verificar gh/tea e importar logins\n" +
			"  " + boldStyle.Render("i") + "  cadastrar instâncias manualmente\n"
	case len(cfg.Sources()) == 0:
		return "\nNenhuma origem Gitea/Forgejo configurada (só GitHub não basta: ele não faz push mirror).\n\n" +
			"  " + boldStyle.Render("i") + "  cadastrar uma instância Gitea/Forgejo\n"
	case d.loading:
		return "\n" + d.spin.View() + " descobrindo push mirrors nas origens…"
	}

	out := ""
	if d.confirm != nil {
		out = warnStyle.Render("? "+d.confirm.prompt+" [y/N]") + "\n"
	} else if d.status != "" {
		out = d.status + "\n"
	} else {
		out = helpStyle.Render(fmt.Sprintf("%d mirror(s) em %d origem(ns)", len(d.links), len(d.sources))) + "\n"
	}
	if len(d.links) == 0 {
		out += "\nNenhum push mirror encontrado. Pressione " + boldStyle.Render("n") + " para criar o primeiro.\n"
	} else {
		out += d.table.View() + "\n"
	}
	for _, err := range d.errs {
		out += warnStyle.Render("aviso: "+truncate(oneLine(err.Error()), d.sh.width-8)) + "\n"
	}
	return out
}
