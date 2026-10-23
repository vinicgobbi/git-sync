package tui

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/vinicgobbi/git-sync/internal/update"
)

type (
	// updateCheckedMsg é a consulta silenciosa feita ao abrir a TUI.
	updateCheckedMsg struct{ r update.Release }
	updateInfoMsg    struct {
		r        update.Release
		notes    []update.Note
		notesErr error
		err      error
	}
	updateAppliedMsg struct {
		target string
		err    error
	}
)

// checkUpdate consulta a última release em segundo plano, para o cabeçalho
// avisar quando há uma nova. Falhas são silenciosas.
func checkUpdate(sh *shared) tea.Cmd {
	if !update.Updatable(sh.version) || os.Getenv("GIT_SYNC_NO_UPDATE_CHECK") != "" {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(sh.ctx, 5*time.Second)
		defer cancel()
		r, err := sh.updates.Latest(ctx)
		if err != nil {
			return nil
		}
		return updateCheckedMsg{r}
	}
}

// updateAvailable é a versão nova já encontrada, ou "".
func (sh *shared) updateAvailable() string {
	if sh.latest != nil && update.Newer(sh.version, sh.latest.Version) {
		return sh.latest.Version
	}
	return ""
}

func updateItem(sh *shared) item {
	return item{
		label: "Verificar atualizações", view: -1,
		run: func() tea.Cmd { return open(newUpdateFlow(sh)) },
		desc: func() string {
			s := "Consulta a última release no GitHub e, se houver versão nova, mostra o que muda e oferece " +
				"baixar. O download é conferido pelo SHA-256 publicado na release e substitui o git-sync do " +
				"PATH (ou este binário, se não há um no PATH).\n\nEsta versão: " + update.Tag(sh.version) + "."
			if v := sh.updateAvailable(); v != "" {
				s += "\nDisponível: " + v + "."
			}
			return s
		},
		cli: "git-sync update [--check]",
	}
}

// updateFlow consulta a última release, mostra o que muda e atualiza.
type updateFlow struct {
	sh       *shared
	spin     spinner.Model
	checking bool
	info     *updateInfoMsg
	applying bool
	applied  *updateAppliedMsg
}

var ukApply = bind([]string{"enter"}, "enter", "atualizar")

func newUpdateFlow(sh *shared) *updateFlow {
	return &updateFlow{sh: sh, spin: spinner.New(spinner.WithSpinner(spinner.MiniDot))}
}

func (f *updateFlow) Title() string   { return "Verificar atualizações" }
func (f *updateFlow) Capturing() bool { return false }

func (f *updateFlow) Init() tea.Cmd {
	f.checking = true
	sh := f.sh
	return tea.Batch(f.spin.Tick, func() tea.Msg {
		ctx, cancel := context.WithTimeout(sh.ctx, 30*time.Second)
		defer cancel()
		r, err := sh.updates.Latest(ctx)
		if err != nil {
			return updateInfoMsg{err: err}
		}
		msg := updateInfoMsg{r: r}
		if update.Newer(sh.version, r.Version) || !update.Updatable(sh.version) {
			msg.notes, msg.notesErr = sh.updates.Changes(ctx, sh.version, r.Version)
		}
		return msg
	})
}

// canApply: há versão nova e esta build sabe se atualizar.
func (f *updateFlow) canApply() bool {
	return f.info != nil && f.info.err == nil && f.applied == nil && !f.applying &&
		update.Updatable(f.sh.version) && update.Newer(f.sh.version, f.info.r.Version)
}

func (f *updateFlow) Keys() []key.Binding {
	switch {
	case f.checking || f.applying:
		return nil
	case f.canApply():
		return []key.Binding{ukApply, kBack}
	}
	return []key.Binding{kBack}
}

func (f *updateFlow) Update(msg tea.Msg) (view, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if f.checking || f.applying {
			var cmd tea.Cmd
			f.spin, cmd = f.spin.Update(msg)
			return f, cmd
		}
	case updateInfoMsg:
		f.checking, f.info = false, &msg
		if msg.err == nil {
			f.sh.latest = &msg.r
		}
	case updateAppliedMsg:
		f.applying, f.applied = false, &msg
		if msg.err == nil {
			f.sh.latest = nil // o cabeçalho para de anunciar a versão nova
		}
	case tea.KeyMsg:
		switch {
		case f.checking || f.applying:
		case key.Matches(msg, ukApply) && f.canApply():
			return f, f.apply()
		case key.Matches(msg, kBack), msg.String() == "enter":
			return f, closeFlow()
		}
	}
	return f, nil
}

func (f *updateFlow) apply() tea.Cmd {
	f.applying = true
	sh, r := f.sh, f.info.r
	return tea.Batch(f.spin.Tick, func() tea.Msg {
		if sh.installer == nil {
			return updateAppliedMsg{err: errNoInstaller}
		}
		ctx, cancel := context.WithTimeout(sh.ctx, 2*time.Minute)
		defer cancel()
		target, err := sh.updates.Apply(ctx, r, sh.installer)
		return updateAppliedMsg{target, err}
	})
}

var errNoInstaller = errors.New("não consegui descobrir onde este binário está instalado")

func (f *updateFlow) View(w, h int) string {
	if f.checking {
		return "\n" + f.spin.View() + " Consultando a última release…"
	}
	var b strings.Builder
	b.WriteString("\n")
	if f.info.err != nil {
		hf := humanize(f.info.err)
		b.WriteString(sErr.Render("✘ "+hf.msg) + "\n")
		if hf.hint != "" {
			b.WriteString(sMuted.Render(wrap(hf.hint, w)) + "\n")
		}
		return b.String()
	}
	cur, latest := update.Tag(f.sh.version), f.info.r.Version
	b.WriteString(sLabel.Render("Esta versão") + cur + "\n")
	b.WriteString(sLabel.Render("Última release") + latest + "\n\n")

	switch {
	case !update.Updatable(f.sh.version):
		b.WriteString(sWarn.Render("⚠ ") + wrap("Esta é uma build de desenvolvimento, que não se atualiza sozinha. "+
			"Baixe a release em "+update.ReleasesURL+".", w-2) + "\n")
	case f.applying:
		b.WriteString(f.spin.View() + " Baixando " + latest + " e conferindo o SHA-256…\n")
	case f.applied != nil && f.applied.err != nil:
		hf := humanize(f.applied.err)
		b.WriteString(sErr.Render("✘ ") + wrap(hf.msg, w-2) + "\n")
	case f.applied != nil:
		b.WriteString(sOK.Render("✔ ") + wrap("Atualizado para "+latest+" em "+f.applied.target+
			". Saia (q) e abra de novo para usar a versão nova.", w-2) + "\n")
		return b.String()
	case !update.Newer(f.sh.version, latest):
		b.WriteString(sOK.Render("✔ Está na versão mais recente.") + "\n")
		return b.String()
	default:
		target := "o git-sync do PATH"
		if f.sh.installer != nil {
			target = f.sh.installer.UpdateTarget()
		}
		b.WriteString(keyHint("enter", "baixa "+latest+" e substitui "+target) + "\n")
	}

	switch {
	case f.info.notesErr != nil:
		b.WriteString("\n" + sMuted.Render(wrap("Não consegui baixar as notas da versão; veja em "+update.ReleasesURL+".", w)) + "\n")
	case len(f.info.notes) > 0:
		title := "O que muda em " + latest
		if len(f.info.notes) > 1 {
			title = "O que muda de " + cur + " para " + latest
		}
		b.WriteString(sSection.Render(title) + "\n" + wrap(update.FormatNotes(f.info.notes, 0), w) + "\n")
	}
	return b.String()
}
