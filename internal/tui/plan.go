package tui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/vinicgobbi/git-sync/internal/mirror"
)

type planPhase int

const (
	phasePlanning planPhase = iota
	phaseReview
	phaseRunning
	phaseDone
)

// planScreen monta o plano (somente leitura), mostra para revisão e executa.
type planScreen struct {
	sh     *shared
	opts   options
	req    *mirror.Request
	plan   *mirror.Plan
	phase  planPhase
	state  []stepState
	table  table.Model
	spin   spinner.Model
	err    error
	sum    mirror.Summary
	events chan tea.Msg
}

type stepState struct {
	status mirror.Status
	msg    string
	seen   bool // recebeu algum evento
}

type (
	plannedMsg struct {
		req  *mirror.Request
		plan *mirror.Plan
		err  error
	}
	execEventMsg struct{ ev mirror.Event }
	execDoneMsg  struct{ sum mirror.Summary }
)

var planColumns = []table.Column{
	{Title: "Repositório", Width: 28},
	{Title: "Destino", Width: 12},
	{Title: "Ação", Width: 12},
	{Title: "Detalhes", Width: 0},
}

func newPlanScreen(sh *shared, opts options) *planScreen {
	return &planScreen{sh: sh, opts: opts, table: newTable(planColumns), spin: spinner.New(spinner.WithSpinner(spinner.Dot))}
}

func (p *planScreen) Title() string { return "Plano" }
func (p *planScreen) Keys() string {
	switch p.phase {
	case phaseReview:
		if p.runnable() > 0 {
			return "enter executar • ↑/↓ rolar • esc voltar"
		}
		return "esc voltar"
	case phaseRunning:
		return "executando…"
	case phaseDone:
		return "enter voltar ao painel"
	}
	return "esc voltar"
}

func (p *planScreen) Init() tea.Cmd {
	sh, o := p.sh, p.opts
	return tea.Batch(p.spin.Tick, func() tea.Msg {
		req := &mirror.Request{
			Source: o.source, SourceForge: o.src, Repos: o.repos,
			Interval: o.interval, SyncOnCommit: o.onCommit, Visibility: o.visibility,
			Force: o.force, Concurrency: sh.env.Cfg.Defaults.Concurrency,
		}
		for _, t := range o.targets {
			tgt, err := sh.env.BuildTarget(sh.ctx, t.name, t.owner)
			if err != nil {
				return plannedMsg{err: err}
			}
			req.Targets = append(req.Targets, tgt)
		}
		plan, err := mirror.BuildPlan(sh.ctx, req)
		return plannedMsg{req: req, plan: plan, err: err}
	})
}

func (p *planScreen) runnable() int {
	if p.plan == nil {
		return 0
	}
	return p.plan.Count(mirror.ActionCreate) + p.plan.Count(mirror.ActionReuse)
}

func waitFor(ch chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		if m, ok := <-ch; ok {
			return m
		}
		return nil
	}
}

func (p *planScreen) Update(msg tea.Msg) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.table.SetColumns(fitColumns(planColumns, msg.Width))
		p.table.SetHeight(max(p.sh.bodyHeight()-4, 3))
	case spinner.TickMsg:
		if p.phase == phasePlanning || p.phase == phaseRunning {
			var cmd tea.Cmd
			p.spin, cmd = p.spin.Update(msg)
			return p, cmd
		}
	case plannedMsg:
		p.phase = phaseReview
		p.req, p.plan, p.err = msg.req, msg.plan, msg.err
		if p.plan != nil {
			p.state = make([]stepState, len(p.plan.Steps))
			setRows(&p.table, p.rows())
		}
	case execEventMsg:
		st := &p.state[msg.ev.Index]
		st.status, st.msg, st.seen = msg.ev.Status, msg.ev.Msg, true
		setRows(&p.table, p.rows())
		return p, waitFor(p.events)
	case execDoneMsg:
		p.phase, p.sum = phaseDone, msg.sum
		setRows(&p.table, p.rows())
	case tea.KeyMsg:
		return p.handleKey(msg)
	}
	return p, nil
}

func (p *planScreen) handleKey(msg tea.KeyMsg) (screen, tea.Cmd) {
	switch p.phase {
	case phaseRunning:
		return p, nil // não dá para sair no meio da execução
	case phaseDone:
		if msg.String() == "enter" || msg.String() == "esc" || msg.String() == "q" {
			return p, popToRoot()
		}
	case phaseReview, phasePlanning:
		switch msg.String() {
		case "esc", "q":
			return p, pop()
		case "enter":
			if p.phase == phaseReview && p.runnable() > 0 {
				return p, p.execute()
			}
			return p, nil
		}
	}
	var cmd tea.Cmd
	p.table, cmd = p.table.Update(msg)
	return p, cmd
}

func (p *planScreen) execute() tea.Cmd {
	p.phase = phaseRunning
	p.events = make(chan tea.Msg, 64)
	ch, req, plan, ctx := p.events, p.req, p.plan, p.sh.ctx
	opts := mirror.ExecOptions{Rollback: p.opts.rollback}
	go func() {
		sum := mirror.Execute(ctx, req, plan, opts, func(ev mirror.Event) { ch <- execEventMsg{ev} })
		ch <- execDoneMsg{sum}
		close(ch)
	}()
	return tea.Batch(p.spin.Tick, waitFor(ch))
}

func (p *planScreen) rows() []table.Row {
	rows := make([]table.Row, len(p.plan.Steps))
	for i, s := range p.plan.Steps {
		action := s.Action.String()
		detail := s.TargetURL
		if s.Reason != "" {
			detail += " · " + s.Reason
		}
		if st := p.state[i]; st.seen {
			switch st.status {
			case mirror.StatusRunning:
				action = "… " + action
				detail = st.msg
			case mirror.StatusDone:
				action = "✔ ok"
				detail = s.TargetURL
			case mirror.StatusFailed:
				action = "✘ falhou"
				detail = oneLine(st.msg)
			case mirror.StatusSkipped:
				action = "– " + action
			}
		}
		rows[i] = table.Row{s.Repo.FullName(), p.req.Targets[s.Target].Instance.Name, action, detail}
	}
	return rows
}

func (p *planScreen) View() string {
	switch {
	case p.phase == phasePlanning:
		return "\n" + p.spin.View() + " consultando origem e destinos para montar o plano…"
	case p.err != nil:
		return "\n" + errStyle.Render("✘ "+p.err.Error()) + "\n"
	}

	var head string
	pl := p.plan
	switch p.phase {
	case phaseReview:
		head = fmt.Sprintf("%s criar · %s reaproveitar · %s já existe · %s conflito · %s inválido",
			okStyle.Render(fmt.Sprint(pl.Count(mirror.ActionCreate))),
			okStyle.Render(fmt.Sprint(pl.Count(mirror.ActionReuse))),
			helpStyle.Render(fmt.Sprint(pl.Count(mirror.ActionSkip))),
			warnStyle.Render(fmt.Sprint(pl.Count(mirror.ActionConflict))),
			errStyle.Render(fmt.Sprint(pl.Count(mirror.ActionInvalid))))
		if p.runnable() == 0 {
			head += "\n" + helpStyle.Render("nada a fazer.")
		} else {
			head += "\n" + boldStyle.Render(fmt.Sprintf("enter para executar %d passo(s)", p.runnable()))
			if p.opts.force && pl.Count(mirror.ActionReuse) > 0 {
				head += warnStyle.Render("  (force ativo: repos com conteúdo serão sobrescritos)")
			}
		}
	case phaseRunning:
		done := 0
		for _, s := range p.state {
			if s.seen && s.status != mirror.StatusRunning {
				done++
			}
		}
		head = fmt.Sprintf("%s executando… %d/%d\n", p.spin.View(), done, len(p.state))
	case phaseDone:
		head = fmt.Sprintf("concluído: %s · %s · %s\n",
			okStyle.Render(fmt.Sprintf("%d ok", p.sum.Done)),
			errStyle.Render(fmt.Sprintf("%d falha(s)", p.sum.Failed)),
			helpStyle.Render(fmt.Sprintf("%d pulado(s)", p.sum.Skipped)))
	}
	return "\n" + head + "\n" + p.table.View()
}
