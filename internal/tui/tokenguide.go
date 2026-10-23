package tui

import (
	"strings"
	"unicode/utf8"

	"github.com/vinicgobbi/git-sync/internal/config"
)

// tokenGuide explica onde criar o token de uma instância e quais permissões
// dar. mirror escolhe o token de mirror (gravado nos espelhos que enviam para
// a instância) em vez do token de acesso (usado pelo git-sync na API).
// As permissões seguem o que o git-sync chama: ver internal/forge.
func tokenGuide(kind config.Kind, base string, mirror bool) (title string, lines []string) {
	if base == "" {
		base = "<endereço>"
	}
	switch {
	case kind == config.KindGitHub && !mirror:
		return "Token de acesso do GitHub", []string{
			"Usado pelo git-sync para listar contas e criar os repositórios de destino.",
			"",
			"Crie em " + base + "/settings/personal-access-tokens/new (fine-grained):",
			"• Resource owner: sua conta, ou a organização onde os repositórios serão criados",
			"• Repository access: All repositories",
			"• Repository permissions:",
			"    Administration: Read and write (criar repositórios)",
			"    Contents: Read-only (ver se o destino já tem conteúdo)",
			"    Metadata: Read-only (automática)",
			"• Expiration: a mais longa que sua conta ou organização permitir",
		}
	case kind == config.KindGitHub:
		return "Token de mirror do GitHub (obrigatório)", []string{
			"Fica gravado nos espelhos: é com ele que o servidor de origem envia para o GitHub.",
			"Por isso é um token separado, só com o necessário para enviar.",
			"",
			"Crie em " + base + "/settings/personal-access-tokens/new (fine-grained):",
			"• Resource owner e Repository access: os mesmos do token de acesso",
			"• Repository permissions:",
			"    Contents: Read and write",
			"    Workflows: Read and write (sem ela, pushes que mudam .github/workflows falham)",
			"• Expiration: a mais longa permitida",
			"",
			`Quando vencer, gere outro, cole aqui e use "Renovar credencial" em Instâncias.`,
		}
	case !mirror:
		name := kindName(kind)
		if kind == "" {
			name = "Gitea/Forgejo"
		}
		return "Token de acesso do " + name, []string{
			"Usado pelo git-sync para listar repositórios, criar destinos e configurar os espelhos.",
			"",
			"Crie em " + base + "/user/settings/applications, em \"Gerar novo token\":",
			"• repository: Leitura e escrita",
			"• organization: Leitura e escrita (Leitura basta se não for criar repositórios em organizações)",
			"• user: Leitura",
			"",
			"Nas origens, sua conta precisa ser administradora dos repositórios espelhados.",
		}
	}
	return "Token de mirror (opcional)", []string{
		"Fica gravado nos espelhos que enviam para esta instância. Sem ele, os espelhos",
		"usam o token de acesso, que pode bem mais do que enviar commits.",
		"",
		"Crie em " + base + "/user/settings/applications, em \"Gerar novo token\":",
		"• repository: Leitura e escrita (nada mais)",
	}
}

// tokenOverview resume os tokens que uma instância do tipo precisa.
func tokenOverview(kind config.Kind) string {
	if kind == config.KindGitHub {
		return "O GitHub precisa de dois tokens: um de acesso, para o git-sync criar os repositórios, " +
			"e um de mirror, que fica gravado nos espelhos. Vá até cada campo para ver como criá-los."
	}
	return "Gitea e Forgejo precisam de um token de acesso. O token de mirror é opcional e recomendado " +
		"se a instância for destino. Vá até cada campo para ver como criá-los."
}

func renderGuide(title string, lines []string, w int) string {
	wrapped := make([]string, len(lines))
	for i, l := range lines {
		indent := utf8.RuneCountInString(l) - utf8.RuneCountInString(strings.TrimLeft(l, " •"))
		wrapped[i] = wrap(l, w-indent)
		if indent > 0 {
			// continuação alinhada ao texto depois do marcador
			wrapped[i] = strings.ReplaceAll(wrapped[i], "\n", "\n"+strings.Repeat(" ", indent))
		}
	}
	return sBold.Render(title) + "\n" + strings.Join(wrapped, "\n")
}
