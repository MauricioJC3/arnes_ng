# arnes

Un **arnés de IA** (agent harness) para la terminal, escrito en Go. Un modelo (LLM)
corre en un bucle de agente: leés en la terminal, el modelo decide si responder o
ejecutar herramientas (shell, archivos, memoria), y cada uso de herramienta pasa
por una aprobación humana.

Inspirado en la idea de *harness engineering* (Gentleman Programming, Mitchell
Hashimoto, Anthropic): lo que rinde no es solo el modelo, sino la infraestructura
que lo envuelve.

## Qué hace

- **Multi-proveedor** detrás de una interfaz común: Anthropic, DeepSeek, Kimi
  (Moonshot) y OpenAI. Se cambia en caliente con `/connect` y queda guardado.
- **Herramientas base**: `bash`, `read_file`, `write_file`, `edit_file`
  (reemplazo quirúrgico de texto), y memoria persistente (`remember` / `recall`).
- **Gateway de aprobación**: nada se ejecuta sin un sí. Denegar no rompe el bucle.
- **Modos de permisos**: `normal` (pregunta), `auto` (ejecuta todo), `plan`
  (solo lectura, el modelo propone un plan). Se ciclan con `shift+tab`.
- **Reglas del proyecto**: si hay un `AGENTS.md` (o `agent.md` / `.arnes/agent.md`)
  en el directorio, su contenido se inyecta al system prompt.
- **Costo en vivo e historial**: la barra de estado muestra el gasto acumulado de
  la sesión (`$0.0421`); `/cost` lista el gasto por sesión, con total. El uso se
  persiste, así que `/resume` continúa el conteo.
- **Sesiones persistentes**: cada turno se guarda; se reanudan por id o prefijo.
- **Compactación de contexto**: `none`, `sliding`, `summarize`, con umbral de tokens.
- **Subagentes**: delegación a agentes especializados (`research`, `test-writer`),
  sin recursión.
- **MCP** (Model Context Protocol): conecta servidores MCP por stdio y expone sus
  herramientas como nativas.
- **TUI** con [Bubble Tea](https://github.com/charmbracelet/bubbletea): streaming
  de tokens en vivo, scroll, autocompletado de comandos, markdown renderizado con
  [glamour](https://github.com/charmbracelet/glamour). También un REPL de línea
  como fallback (`ARNES_UI=plain`).

## Uso

```bash
make build      # compila ./arnes  (go run recompila cada vez)
./arnes         # arranca la TUI

# primera vez: elegí proveedor, modelo y API key
/connect        # abre el picker interactivo
```

La config queda en `~/.arnes/config.json` (permisos `0600`, tiene API keys).

### Variables de entorno

Las variables ganan sobre el archivo de config.

| Variable | Efecto |
|---|---|
| `ARNES_PROVIDER` | `anthropic` (default) `\|` `deepseek` `\|` `kimi` `\|` `openai` |
| `ARNES_MODEL` | override del modelo |
| `ARNES_UI` | `tui` (default) `\|` `plain` |
| `ARNES_STREAM` | `off` para desactivar el streaming en la TUI |
| `ARNES_MOUSE` | `on` para captura de mouse (rueda de scroll; rompe el copiado nativo) |
| `ARNES_COMPACT` | `off` (default) `\|` `sliding` `\|` `summarize` |
| `ARNES_COMPACT_AT` | umbral de tokens para auto-compactar (default 120000) |
| `ARNES_RESUME` | id (o prefijo) de sesión a reanudar al arrancar |
| `ARNES_RULES` | ruta a un archivo de reglas del proyecto (default: `AGENTS.md` en el cwd) |
| `ARNES_CONFIG` / `ARNES_THEME` / `ARNES_MCP` / `ARNES_SUBAGENTS` | rutas alternativas |
| `ANTHROPIC_API_KEY` / `DEEPSEEK_API_KEY` / `MOONSHOT_API_KEY` / `OPENAI_API_KEY` | API keys por entorno |

### Comandos (slash)

`/help` `/connect` `/mode` `/cost` `/model` `/sessions` `/resume` `/new`
`/compact` `/subagents` `/exit`. Escribí `/` en la TUI para el autocompletado.

### Teclas (TUI)

`Enter` enviar · `Esc` cancelar el turno en curso · `shift+tab` ciclar modo ·
`↑↓` scroll (con el input vacío) · `PgUp/PgDn` `Ctrl+U/Ctrl+D` `Home/End` scroll ·
`Ctrl+C` salir.

## Estructura

| Paquete | Responsabilidad |
|---|---|
| `cmd/arnes` | wiring: config, proveedor, herramientas, modos |
| `internal/agent` | el bucle del agente (REPL externo / tool loop interno) |
| `internal/provider` | puerto `Provider` + adaptadores (Anthropic SDK, HTTP OpenAI-compatible) |
| `internal/tool` | herramientas base + `Registry` |
| `internal/approval` | gateway de aprobación (prompt, canal async, allow-all, read-only) |
| `internal/session` | persistencia de conversaciones (`FileStore`, decorator `Persisting`) |
| `internal/memory` | memoria persistente (`remember` / `recall`) |
| `internal/compact` | estrategias de compactación de contexto |
| `internal/subagent` | definiciones de subagentes + `DelegateTool` |
| `internal/mcp` | cliente MCP stdio (JSON-RPC 2.0) |
| `internal/command` | dispatcher de slash commands (compartido TUI / REPL) |
| `internal/tui` | front-end Bubble Tea |
| `internal/repl` | front-end de línea (fallback) |

## Desarrollo

```bash
make test    # go test ./...
make vet     # go vet ./...
```

Go 1.26. TDD: cada paquete tiene sus tests, `go test ./...` corre offline
(los providers reales se testean con mocks / `httptest`).
