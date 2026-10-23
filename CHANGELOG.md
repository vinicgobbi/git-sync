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
