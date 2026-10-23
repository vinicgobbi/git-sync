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
abre o cadastro da primeira conta, mostrando como criar os tokens (veja [Credenciais](#credenciais)).

As ações ficam num menu à esquerda (Espelhos, Instâncias e Esta ferramenta). O painel à direita mostra
a tela do item selecionado ou, numa ação, o que ela faz e o comando equivalente. Sob o título, uma linha
mostra **o próximo passo** (cadastrar a origem, o destino, criar o primeiro espelho, ver os que falharam...).
`↑↓` navega, `enter` abre (ou passa o foco para a tela), `esc` volta ao menu e `q` sai.

- **Espelhos**: todos os push mirrors das origens (inclusive os criados fora da ferramenta), com
  status, último envio e dicas para cada erro. `n` cria um espelho, `s` sincroniza, `o` abre no navegador,
  `x` remove, `/` filtra.
- **Novo espelho**: assistente em etapas (origem → repositórios → destinos → opções → revisão → execução).
  Nada é alterado antes da revisão, e rodar de novo não duplica nada.
- **Instâncias**: contas cadastradas, testadas na hora; `a` adiciona, `enter` edita, `c` renova a
  credencial dos espelhos que enviam para a instância.
- **Diagnóstico**: keyring do sistema e arquivo de configuração.

`?` mostra todos os atalhos da tela atual.

A mesma funcionalidade existe como CLI:

```sh
git-sync doctor                 # verifica o keyring e o acesso de cada instância
git-sync instance add --name casa --url https://git.casa.lan
git-sync instance add --name codeberg --url https://codeberg.org --mirror-token
git-sync instance add --name github --url https://github.com   # pede também o token de mirror
git-sync instance renew github  # grava o token atual nos espelhos que enviam para o GitHub
git-sync discover               # lista push mirrors existentes
git-sync mirror --from casa --repo eu/app --to github --to codeberg:minha-org --dry-run
git-sync mirror --from casa --all --owner eu --to github
git-sync install [--dir pasta] [--add-to-path | --no-path]
git-sync update [--check]
```

A configuração fica em `~/.config/git-sync/config.yaml`.

### Credenciais

Toda a autenticação é feita por **tokens**, guardados no keyring do sistema (nunca no arquivo de
configuração). Cada instância cadastrada tem até dois:

- **Token de acesso**: usado pelo próprio git-sync na API da instância, para listar repositórios e
  organizações, criar os repositórios de destino e configurar os espelhos nas origens.
- **Token de mirror**: não é usado pelo git-sync, e sim **gravado em cada espelho**, no servidor de
  origem, que o usa para enviar os commits ao destino. Como fica guardado em outro servidor, deve poder
  só enviar commits. Só importa para instâncias que são **destino**.

Na interface, o formulário de cada instância mostra estas mesmas instruções para o campo em foco. Na
CLI (`git-sync instance add`), os tokens são pedidos sem eco ou lidos de `GIT_SYNC_TOKEN` e
`GIT_SYNC_MIRROR_TOKEN`.

#### Gitea e Forgejo

Podem ser **origem** (enviam os espelhos) e **destino** (recebem). Os tokens são criados em
`<endereço>/user/settings/applications` (Configurações → Aplicações), na seção de gerar um novo token,
logado com a conta que o git-sync vai usar. Para cada categoria de escopo, escolha o nível indicado e
deixe as demais em "Sem acesso". Se a versão perguntar o alcance do token, escolha **todos** os
repositórios (públicos, privados e limitados), não só os públicos. Tokens do Gitea e do Forgejo não
expiram.

**Token de acesso** (obrigatório)

| Escopo | Nível | Para quê |
|---|---|---|
| `repository` | Leitura e escrita | listar repositórios; na origem, criar, sincronizar e remover os espelhos; no destino, criar os repositórios |
| `organization` | Leitura e escrita | criar repositórios de destino em organizações (**Leitura** basta se a instância for só origem ou se você só criar na sua conta) |
| `user` | Leitura | identificar a conta do token |

Na **origem**, a conta do token precisa ser **administradora** de cada repositório espelhado: o Gitea e
o Forgejo só deixam administradores configurar espelhos. Repositórios em que ela não é administradora
não aparecem para espelhar. O administrador da instância também precisa manter os push mirrors
habilitados (`[mirror] ENABLED`, `DISABLE_NEW_PUSH`); o git-sync avisa em Instâncias quando estão
desligados.

**Token de mirror** (opcional, recomendado se a instância for destino)

| Escopo | Nível | Para quê |
|---|---|---|
| `repository` | Leitura e escrita | receber os commits enviados pela origem |

Sem ele, os espelhos que enviam para esta instância usam o token de acesso, que pode bem mais do que
enviar commits (criar repositórios, mexer em organizações). A conta do token precisa ter escrita nos
repositórios de destino: se forem de uma organização, ela precisa ser membro com escrita.

#### GitHub

Só pode ser **destino**: o GitHub não tem push mirrors. Use tokens **classic**, criados em
`github.com/settings/tokens/new` (Settings → Developer settings → Personal access tokens → Tokens
(classic)); no GitHub Enterprise, `<endereço>/settings/tokens/new`. Os dois são **obrigatórios**.

Por que classic: com tokens fine-grained, a API não lista suas organizações (não dá para escolhê-las
como destino) e um token restrito a alguns repositórios não alcança os que o git-sync cria depois. Eles
são aceitos, mas com aviso em Instâncias.

**Token de acesso** (obrigatório)

| Escopo | Para quê |
|---|---|
| `repo` | criar os repositórios de destino (privados também) e ver se já existem e se estão vazios |
| `read:org` (dentro de `admin:org`; `write:org` ou `admin:org` também servem) | listar suas organizações, para escolher onde criar |
| `delete_repo` (recomendado) | desfazer o repositório recém-criado quando a configuração do espelho falha; sem ele, o repositório vazio fica no GitHub |

**Token de mirror** (obrigatório)

| Escopo | Para quê |
|---|---|
| `repo` | receber os commits enviados pela origem |
| `workflow` | receber commits que alteram `.github/workflows`; sem ele, esses envios são recusados |

Para os dois:

- **Expiration**: escolha "No expiration" ou a validade mais longa que sua conta ou organização
  permitir. Quando um token vencer, veja [Trocar um token](#trocar-um-token).
- **Organizações com SSO (SAML)**: depois de criar o token, autorize-o em cada organização de destino
  (na lista de tokens, Configure SSO → Authorize). Sem isso, o GitHub responde 403 para elas.
- **Organizações que restringem tokens classic** recusam o token mesmo com os escopos certos; nesse
  caso, fale com um administrador da organização.
- O git-sync **confere os escopos** ao salvar a instância e recusa um token incompleto, dizendo o que
  falta. Em Instâncias, avisa quando falta `delete_repo` ou quando o token de mirror ficou sem
  `workflow`.

#### Resumo

| Instância | Token | Obrigatório? | Onde criar | Escopos |
|---|---|---|---|---|
| Gitea/Forgejo | acesso | sim | `<endereço>/user/settings/applications` | `repository` e `organization`: leitura e escrita; `user`: leitura |
| Gitea/Forgejo | mirror | não (recomendado se for destino) | `<endereço>/user/settings/applications` | `repository`: leitura e escrita |
| GitHub | acesso | sim | `github.com/settings/tokens/new` (classic) | `repo`, `read:org`; `delete_repo` recomendado |
| GitHub | mirror | sim | `github.com/settings/tokens/new` (classic) | `repo`, `workflow` |

### Trocar um token

Cada espelho guarda uma **cópia** do token de mirror (ou de acesso) de quando foi criado, e a API não
permite editá-la. Se o token vencer ou for revogado, os envios falham. Depois de cadastrar o token
novo na instância, renove os espelhos: `c` em Instâncias ou em Espelhos, ou `git-sync instance renew
NOME`. Ao editar uma instância pela interface, o git-sync já oferece a renovação. Cada espelho é
recriado com o mesmo endereço, intervalo e envio a cada push; o novo é criado antes de o antigo ser
apagado, então uma falha não deixa o repositório sem espelho.

Configurações antigas com `auth: tea` ou `auth: gh` (o token vinha do login dessas ferramentas) não
são mais suportadas: a instância aparece sem credencial até você editá-la e informar um token.

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
