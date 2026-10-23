package update

import (
	"bufio"
	"context"
	"fmt"
	"regexp"
	"strings"
)

// ChangelogURL é o CHANGELOG.md (escrito pelo `cz bump`) numa tag (%s). Ele
// acumula todas as versões, então um download cobre qualquer salto.
const ChangelogURL = "https://raw.githubusercontent.com/vinicgobbi/git-sync/%s/CHANGELOG.md"

// Note é a seção de uma versão no changelog.
type Note struct {
	Version, Date string
	Sections      []NoteSection
}

type NoteSection struct {
	Title string // em português (Novidades, Correções...); ver sectionNames
	Items []string
}

var (
	versionHeadRe = regexp.MustCompile(`^## (v\d+\.\d+\.\d+)\s*(?:\(([^)]*)\))?`)
	boldScopeRe   = regexp.MustCompile(`^\*\*([^*]+)\*\*:\s*`)
)

// sectionNames traduz os títulos que o Commitizen escreve.
var sectionNames = map[string]string{
	"Feat":            "Novidades",
	"Fix":             "Correções",
	"Refactor":        "Refatoração",
	"Perf":            "Desempenho",
	"BREAKING CHANGE": "Mudanças incompatíveis",
}

// ParseChangelog devolve as notas das versões depois de current até latest,
// da mais nova para a mais antiga. Uma current que não é versão (build de
// desenvolvimento) recebe só as notas de latest.
func ParseChangelog(text, current, latest string) []Note {
	current, latest = Tag(current), Tag(latest)
	var notes []Note
	var note *Note
	var sec *NoteSection
	sc := bufio.NewScanner(strings.NewReader(text))
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), " \t\r")
		if m := versionHeadRe.FindStringSubmatch(line); m != nil {
			note, sec = nil, nil
			if inRange(m[1], current, latest) {
				notes = append(notes, Note{Version: m[1], Date: m[2]})
				note = &notes[len(notes)-1]
			}
			continue
		}
		if note == nil {
			continue
		}
		switch {
		case strings.HasPrefix(line, "### "):
			title := strings.TrimSpace(line[4:])
			if t, ok := sectionNames[title]; ok {
				title = t
			}
			note.Sections = append(note.Sections, NoteSection{Title: title})
			sec = &note.Sections[len(note.Sections)-1]
		case strings.HasPrefix(line, "- ") && sec != nil:
			sec.Items = append(sec.Items, boldScopeRe.ReplaceAllString(strings.TrimSpace(line[2:]), "$1: "))
		case strings.HasPrefix(line, "  ") && sec != nil && len(sec.Items) > 0 && strings.TrimSpace(line) != "":
			// continuação de um item em várias linhas (ex.: corpo de BREAKING CHANGE)
			sec.Items[len(sec.Items)-1] += " " + strings.TrimSpace(line)
		}
	}
	return notes
}

func inRange(v, current, latest string) bool {
	if c, ok := compare(v, latest); !ok || c > 0 {
		return false
	}
	if c, ok := compare(v, current); ok {
		return c > 0
	}
	return v == latest
}

// Changes baixa o changelog de latest e devolve o que mudou desde current.
// Com uma URL própria (testes) e sem ChangelogURL, não há o que baixar:
// devolve nil.
func (c Client) Changes(ctx context.Context, current, latest string) ([]Note, error) {
	tmpl := c.ChangelogURL
	if tmpl == "" {
		if c.URL != "" {
			return nil, nil
		}
		tmpl = ChangelogURL
	}
	b, err := c.get(ctx, fmt.Sprintf(tmpl, Tag(latest)), 4<<20)
	if err != nil {
		return nil, err
	}
	return ParseChangelog(string(b), current, latest), nil
}

// FormatNotes escreve as notas como texto, em até maxLines itens (0 = sem
// limite); o resto é indicado pela página das releases.
func FormatNotes(notes []Note, maxLines int) string {
	var b strings.Builder
	shown, total := 0, 0
	for _, n := range notes {
		for _, s := range n.Sections {
			total += len(s.Items)
		}
	}
	for _, n := range notes {
		if maxLines > 0 && shown >= maxLines {
			break
		}
		head := n.Version
		if n.Date != "" {
			head += "  (" + n.Date + ")"
		}
		b.WriteString(head + "\n")
		for _, s := range n.Sections {
			if maxLines > 0 && shown >= maxLines {
				break
			}
			b.WriteString("  " + s.Title + "\n")
			for _, it := range s.Items {
				if maxLines > 0 && shown >= maxLines {
					break
				}
				b.WriteString("    • " + it + "\n")
				shown++
			}
		}
	}
	if shown < total {
		fmt.Fprintf(&b, "  … e mais %d mudança(s): %s\n", total-shown, ReleasesURL)
	}
	return strings.TrimRight(b.String(), "\n")
}
