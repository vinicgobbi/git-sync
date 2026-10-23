# git-sync

Gerencia **push mirrors** de repositórios Gitea/Forgejo para GitHub, Gitea ou Forgejo.

A origem é sempre um Gitea ou Forgejo, que espelham nativamente ("Opções de Espelhamento").
O git-sync cria o repositório vazio no destino e configura o push mirror na origem apontando para ele.

> Status: núcleo + CLI funcionando. A interface TUI está em desenvolvimento.

## Instalação

```sh
go install github.com/vinicgobbi/git-sync/cmd/git-sync@latest
```

## Uso

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
