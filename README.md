# git-sync

Gerencia **push mirrors** de repositórios Gitea/Forgejo para GitHub, Gitea ou Forgejo.

A origem é sempre um Gitea ou Forgejo, que espelham nativamente ("Opções de Espelhamento").
O git-sync cria o repositório vazio no destino e configura o push mirror na origem apontando para ele.

## Instalação

Baixe o binário da sua plataforma (Linux, macOS ou Windows; amd64/arm64) em
[Releases](https://github.com/vinicgobbi/git-sync/releases), ou:

```sh
go install github.com/vinicgobbi/git-sync/cmd/git-sync@latest
```

## Uso

Sem argumentos, `git-sync` abre a interface (TUI):

- **Painel**: lista os push mirrors de todas as origens (inclusive os criados fora da ferramenta),
  com último sync e erros; `s` sincroniza agora, `x` remove o mirror.
- **Doctor** (`d`): verifica git/gh/tea e importa logins do tea e do gh (`m`).
- **Instâncias** (`i`): cadastra, edita ou remove instâncias e tokens.
- **Novo espelho** (`n`): escolhe repositórios (com filtro e seleção múltipla), destinos e opções,
  revisa o plano e executa. Rodar de novo não duplica nada.

A mesma funcionalidade existe como CLI:

```sh
git-sync doctor                 # verifica git, gh, tea, logins e tokens
git-sync instance import        # importa logins do tea e do gh
git-sync instance add --name codeberg --url https://codeberg.org [--mirror-token]
git-sync discover               # lista push mirrors existentes
git-sync mirror --from casa --repo eu/app --to github --to codeberg:minha-org --dry-run
git-sync mirror --from casa --all --owner eu --to github
```

Os tokens ficam no keyring do sistema e a configuração em `~/.config/git-sync/config.yaml`.

### Tokens de mirror

O token do destino fica gravado no servidor de origem. Prefira um token dedicado
(`--mirror-token`). No GitHub, use um fine-grained PAT com **Contents: write** e
**Workflows: write** (sem este, pushes que alteram `.github/workflows` falham).

## Desenvolvimento

### Commits e versões

O projeto usa [Conventional Commits](https://www.conventionalcommits.org/pt-br/) e
[commitizen](https://commitizen-tools.github.io/commitizen/) (`pipx install commitizen`; `cz commit` ajuda a escrever a mensagem).

A cada push na `main`, a Action decide o bump pelas mensagens desde a última tag:
`fix:` → patch, `feat:` → minor, `BREAKING CHANGE`/`feat!:` → major. Havendo bump, ela atualiza o
`CHANGELOG.md`, cria o commit `bump: …` e a tag `vX.Y.Z`, e publica a release com os binários
(GoReleaser). Commits `docs:`, `ci:`, `chore:`, `refactor:`, `test:` não geram release.
Em PRs, as mensagens de commit são validadas com `cz check`.

### Testes

```sh
go test ./...
```

O teste `TestE2E` (em `internal/tui`) dirige a TUI contra um Gitea e um Forgejo reais.
Ele roda só quando `GITSYNC_E2E_GITEA_URL`, `GITSYNC_E2E_GITEA_TOKEN`, `GITSYNC_E2E_FORGEJO_URL`
e `GITSYNC_E2E_FORGEJO_TOKEN` estão definidas. A origem precisa ter os repos `vini/app` e `vini/lib`,
e a origem precisa alcançar o destino (com `[migrations] ALLOW_LOCALNETWORKS = true` se for rede local).
