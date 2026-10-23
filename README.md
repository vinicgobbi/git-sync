# git-sync

Gerencia **push mirrors** de repositórios Gitea/Forgejo para GitHub, Gitea ou Forgejo.

A origem é sempre um Gitea ou Forgejo, que espelham nativamente ("Opções de Espelhamento").
O git-sync cria o repositório vazio no destino e configura o push mirror na origem apontando para ele.

## Instalação

Baixe o binário da sua plataforma em [Releases](https://github.com/vinicgobbi/git-sync/releases)
(`git-sync_linux_amd64`, `git-sync_darwin_arm64`, `git-sync_windows_amd64.exe`…; o
`checksums.txt` tem o SHA-256 de cada um) e instale no seu PATH, sem admin:

```sh
curl -fLo git-sync https://github.com/vinicgobbi/git-sync/releases/latest/download/git-sync_linux_amd64
chmod +x git-sync
./git-sync install                        # copia para ~/.local/bin/git-sync
./git-sync install --dir /usr/local/bin   # pasta própria (pode exigir sudo)
git-sync_windows_amd64.exe install        # Windows: %LOCALAPPDATA%\Programs\git-sync
```

Se a pasta não estiver no PATH, o `install` oferece adicionar a linha no `~/.zshrc`, `~/.bashrc` ou
`config.fish` (conforme o seu shell) ou, no Windows, ao PATH do usuário. Se já houver outro `git-sync`
no PATH, ele é substituído por esta versão. A mesma opção aparece na interface ("Instalar no PATH")
enquanto o binário aberto não for o instalado.

Ou, com Go:

```sh
go install github.com/vinicgobbi/git-sync/cmd/git-sync@latest
```

### Atualização

`git-sync update` mostra o que mudou desde a sua versão (o changelog de cada release), baixa a última
release do seu sistema, confere o SHA-256 publicado no `checksums.txt` e substitui o `git-sync` do PATH
(ou o próprio binário, se não houver um no PATH). `git-sync update --check` só verifica. Na interface,
o mesmo fica em "Verificar atualizações". Ao abrir, a interface consulta a última versão em segundo
plano e avisa no cabeçalho quando há uma nova; para desligar a consulta, defina
`GIT_SYNC_NO_UPDATE_CHECK=1`. Builds de desenvolvimento (`dev`) não se atualizam.

## Uso

Sem argumentos, `git-sync` abre a interface (TUI). Na primeira vez, um guia explica o conceito e
encontra seus logins do `tea` e do `gh`.

As ações ficam num menu à esquerda (Espelhos, Instâncias e Esta ferramenta). O painel à direita mostra
a tela do item selecionado ou, numa ação, o que ela faz e o comando equivalente. Sob o título, uma linha
mostra **o próximo passo** (cadastrar a origem, o destino, criar o primeiro espelho, ver os que falharam...).
`↑↓` navega, `enter` abre (ou passa o foco para a tela), `esc` volta ao menu e `q` sai.

- **Espelhos**: todos os push mirrors das origens (inclusive os criados fora da ferramenta), com
  status, último envio e dicas para cada erro. `n` cria um espelho, `s` sincroniza, `o` abre no navegador,
  `x` remove, `/` filtra.
- **Novo espelho**: assistente em etapas (origem → repositórios → destinos → opções → revisão → execução).
  Nada é alterado antes da revisão, e rodar de novo não duplica nada.
- **Instâncias**: contas cadastradas, testadas na hora; `a` adiciona, `enter` edita, `i` importa do tea/gh.
- **Diagnóstico**: git, gh, tea e keyring.

`?` mostra todos os atalhos da tela atual.

A mesma funcionalidade existe como CLI:

```sh
git-sync doctor                 # verifica git, gh, tea, logins e tokens
git-sync instance import        # importa logins do tea e do gh
git-sync instance add --name codeberg --url https://codeberg.org [--mirror-token]
git-sync instance add --name casa --url https://git.casa.lan --auth tea   # usa o login do tea
git-sync discover               # lista push mirrors existentes
git-sync mirror --from casa --repo eu/app --to github --to codeberg:minha-org --dry-run
git-sync mirror --from casa --all --owner eu --to github
git-sync install [--dir pasta] [--add-to-path | --no-path]
git-sync update [--check]
```

A configuração fica em `~/.config/git-sync/config.yaml`.

### Credenciais

Cada instância obtém seu token de uma destas formas:

- **Login do tea** (`auth: tea`): o token é pedido ao `tea login helper` a cada uso, inclusive para
  logins **OAuth** (o tea os renova sozinho). Nada é copiado.
- **Login do gh** (`auth: gh`): o mesmo, via `gh auth token`.
- **Token de acesso**: digitado no git-sync e guardado no keyring do sistema.

### Tokens de mirror

O token do destino fica gravado no servidor de origem. Prefira um token dedicado
(`--mirror-token`). No GitHub, use um fine-grained PAT com **Contents: write** e
**Workflows: write** (sem este, pushes que alteram `.github/workflows` falham).

Logins OAuth expiram em poucas horas, então não servem como credencial de espelho: para usar uma
instância com login OAuth como **destino**, cadastre um token de mirror nela. Como **origem**, o
OAuth funciona sem nada extra.

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
