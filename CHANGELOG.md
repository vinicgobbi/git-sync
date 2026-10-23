## v2.0.1 (2026-10-23)

### Fix

- tokens classic no GitHub, com escopos conferidos ao salvar

## v2.0.0 (2026-10-23)

### BREAKING CHANGE

- os logins do tea e do gh não são mais usados: saem
`auth: tea|gh`, `--auth`, `instance import` e a importação na TUI.
Instâncias configuradas assim ficam sem credencial até receberem um
token de acesso. O pacote deps e a verificação de git, gh e tea também
saem do doctor e do Diagnóstico.

### Feat

- autenticação só por tokens, com guia de permissões e renovação

## v1.4.0 (2026-10-23)

### Feat

- verificar e baixar atualizações; release com binários soltos

## v1.3.0 (2026-10-23)

### Feat

- instalar o binário no PATH (comando install e item na TUI)

## v1.2.0 (2026-10-23)

### Feat

- **tui**: menu lateral e painel no estilo do deploy-tools

## v1.1.0 (2026-10-23)

### Feat

- **tui**: redesenho com foco na experiência do usuário
- credenciais via login do tea (inclusive OAuth) e do gh

## v1.0.0 (2026-10-23)

### Feat

- TUI (bubbletea): painel de mirrors com sincronizar/remover, doctor com importação de logins do tea/gh, cadastro de instâncias, seleção de repositórios com filtro e multiseleção, wizard de destinos/opções e plano com execução ao vivo
- CLI: `doctor`, `instance list|add|rm|import`, `discover`, `mirror` e `version`
- clientes REST para Gitea/Forgejo (push mirrors) e GitHub, tokens no keyring do sistema
- planejamento idempotente de espelhamentos, execução paralela com rollback e auto-descoberta de mirrors existentes
