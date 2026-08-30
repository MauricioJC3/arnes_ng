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
- **Herramientas base**: `bash`, `grep` (busca texto, usa ripgrep si está),
  `glob` (busca archivos, soporta `**`), `read_file`, `write_file`, `edit_file`
  (reemplazo quirúrgico), y memoria persistente por proyecto (`remember` / `recall`).
- **`code_graph`** (opcional): si el CLI `codegraph` está instalado y el proyecto
  tiene índice (`.codegraph/`), aparece una tool de solo lectura para preguntas
  estructurales — `callers`, `callees`, `impact`, `explore`, `query` — en una
  llamada en vez de encadenar greps. Si no está, no se ofrece.
- **Eficiencia**: prompt caching en Anthropic (`cache_control` en system + tools +
  prefijo del historial) y retry con backoff ante 429/5xx.
- **Gateway de aprobación**: nada que escriba o ejecute corre sin un sí. Denegar
  no rompe el bucle. Las tools de solo lectura (`read_file`, `grep`, `glob`,
  `lsp`, `recall`, `skill`) no piden confirmación ni en modo `normal`.
- **Modos de permisos**: `normal` (pregunta por escrituras y comandos), `auto`
  (ejecuta todo), `plan` (solo lectura, el modelo propone un plan). Se ciclan con
  `shift+tab`; `/mode <x>` lo fija y lo guarda en la config. Al arrancar:
  `ARNES_MODE` → `mode` de la config → `normal`.
- **`/goal` (Ralph loop)**: itera autónomamente hacia un objetivo, re-enviando el
  mismo prompt cada vuelta. Corta cuando el modelo responde `ARNES_GOAL_DONE`, al
  llegar al límite de iteraciones (default 15, `/goal 5 <obj>`), si se estanca, o
  con `Ctrl+C`. Conviene combinarlo con el modo `auto`. `/goal --fresh <obj>` usa un
  agente nuevo con contexto vacío cada iteración (estado en archivos/git) — más
  barato para runs largos.
- **Portón de cierre de turno**: un turno que editó algo no puede cerrarse hasta
  que el comando de verificación (`ARNES_CHECK_CMD` / `check_command`) pase — si
  falla, la salida vuelve al modelo y sigue (hasta 3 reintentos). Además, la
  tarea original de la sesión y la checklist viva quedan ancladas al system
  prompt (la compactación no las toca), y si el modelo cierra con tareas sin
  completar recibe un aviso para terminarlas o justificarlas.
- **Leer antes de escribir**: `edit_file` / `write_file` sobre un archivo que ya
  existe y que el modelo no leyó esta sesión se rechaza — tiene que pasar por
  `read_file` primero. Crear un archivo nuevo no necesita lectura previa.
- **Reglas del proyecto**: si hay un `AGENTS.md` (o `agent.md` / `.arnes/agent.md`)
  en el directorio, su contenido se inyecta al system prompt.
- **Costo en vivo e historial**: la barra de estado muestra el gasto acumulado de
  la sesión (`$0.0421`); `/cost` lista el gasto por sesión, con total. El uso se
  persiste, así que `/resume` continúa el conteo.
- **Sesiones persistentes**: cada turno se guarda; se reanudan por id o prefijo.
- **Memoria por proyecto**: `remember` / `recall` guardan notas en
  `~/.arnes/memory/notes.json`, scopeadas al proyecto (remote de git, o la ruta si
  no hay remote). Al arrancar una sesión —o al cambiar de modelo— las notas del
  proyecto se inyectan al system prompt como "Memoria del proyecto", así el
  contexto sobrevive. Si preferís Engram u otra memoria por MCP, agregá su
  servidor a `~/.arnes/mcp.json` y sus tools (`mem_save`, `mem_search`, …) quedan
  disponibles junto a las nativas; las dos cosas conviven.
- **Checkpoints / rewind**: antes de cada turno se toma un punto de restauración
  (historial + contenido previo de los archivos que el turno toque con
  `write_file` / `edit_file`). Si el turno corre `bash` dentro de un repo git,
  además se guarda un baseline (`git stash create`, o `HEAD` si el árbol estaba
  limpio). `/rewind` lista los checkpoints; `/rewind n` vuelve al checkpoint `n`:
  reescribe los archivos, resetea los archivos versionados al baseline con
  `git checkout` — lo que también descarta otros cambios sin commitear en
  archivos versionados — y recorta el historial. Los archivos nuevos quedan.
  Viven en memoria durante el proceso.
- **Compactación de contexto**: `sliding` (default), `summarize` u `off`, con umbral de tokens,
  para que una sesión larga no infle el contexto ni empuje al modelo a loops.
- **Subagentes**: delegación a agentes especializados (`research`, `test-writer`),
  sin recursión.
- **Skills**: archivos `SKILL.md` (mismo formato que Claude Code) en
  `~/.arnes/skills/<nombre>/SKILL.md` y `<proyecto>/.arnes/skills/…` (el del
  proyecto le gana al global). La tool `skill` le muestra al modelo los skills
  disponibles y carga el cuerpo del que elija dentro del turno. En cada arranque
  arnes se asegura de que un set curado que viene en el binario (`docker`,
  `docker-compose`, `software-architecture`, `architecture-review`,
  `tdd-workflow`, `testing-strategy`, `api-design`, `distinctive-web-design`) esté
  en `~/.arnes/skills`: agrega solo los que falten, sin
  preguntar. Si editás uno, tu versión queda (no se sobreescribe); si borrás uno,
  vuelve en el próximo arranque. Tus propios skills no se tocan. Atribución de
  las fuentes en `internal/skill/defaults/NOTICE.md`.
- **MCP** (Model Context Protocol): conecta servidores MCP por stdio y expone sus
  herramientas como nativas.
- **Todo tracking**: la tool `todo_write` mantiene la lista de tareas del trabajo
  actual y la TUI la muestra en vivo, tildando a medida que el modelo avanza.
- **Hooks**: `~/.arnes/hooks.json` corre comandos alrededor de cada tool call —
  `pre_tool` (puede bloquear la llamada, p. ej. tests antes de `git commit`) y
  `post_tool` (reacciona, p. ej. `gofmt -w` tras un edit). Cada hook filtra por un
  regex `match` contra el nombre de la tool y recibe el JSON de la llamada por stdin.
- **LSP**: la tool `lsp` consulta un language server sobre un archivo —
  `diagnostics`, `definition` o `hover`. El server se arranca perezosamente por
  extensión y se configura en `~/.arnes/lsp.json` (`{"servers":{".go":{"command":"gopls"}}}`);
  por defecto usa `gopls` para `.go`.
- **TUI** con [Bubble Tea](https://github.com/charmbracelet/bubbletea): streaming
  de tokens en vivo, scroll, autocompletado de comandos, markdown renderizado con
  [glamour](https://github.com/charmbracelet/glamour). También un REPL de línea
  como fallback (`ARNES_UI=plain`).

## Instalación

Un comando. Baja el binario del último release y lo pone en el PATH.

**Linux / macOS**

```bash
curl -fsSL https://raw.githubusercontent.com/MauricioJC3/arnes_ng/main/install.sh | sh
```

**Windows** (PowerShell)

```powershell
irm https://raw.githubusercontent.com/MauricioJC3/arnes_ng/main/install.ps1 | iex
```

Variables opcionales: `ARNES_INSTALL_DIR` (dónde instalar) y `ARNES_VERSION`
(un tag concreto en lugar del último).

Con Go instalado:

```bash
go install github.com/MauricioJC3/arnes_ng/cmd/arnes@latest
```

Desde el fuente: `make install` (usa `go install`) o `make build` (deja `./arnes`).

## Actualización

- **`/update-arnes`** — busca el último release, y si hay uno más nuevo baja el
  binario de tu plataforma, verifica su SHA-256 y reemplaza el ejecutable en el
  lugar. Después reiniciás arnes.
- **Chequeo diario** — al arrancar, una vez por día, arnes mira si hay una
  versión nueva (sin bloquear el arranque) y te avisa en la conversación. Con
  `auto_update: true` en `~/.arnes/config.json` (o `ARNES_AUTO_UPDATE=on`) el
  chequeo instala la versión nueva solo y te avisa que lo hizo. Por defecto solo
  notifica: reemplazar el binario en caliente sin tu OK es más riesgoso.

## Uso

```bash
make build      # compila ./arnes  (go run recompila cada vez)
./arnes         # arranca la TUI

# primera vez: elegí proveedor, API key y modelo
/connect        # picker interactivo: proveedor → API key → modelo
                # el menú de modelos se trae en vivo del proveedor (endpoint /models);
                # si falla la red o la key, cae a una lista local. Incluye
                # "escribir a mano" para ids que todavía no estén en la lista.
/model          # picker de modelos, agrupado por cada proveedor con key guardada;
                # el actual queda marcado. Elegir uno de otro proveedor cambia a él.
                # /model <nombre> lo setea directo.
```

La config queda en `~/.arnes/config.json` (permisos `0600`, tiene API keys).

### Variables de entorno

Las variables ganan sobre el archivo de config.

| Variable | Efecto |
|---|---|
| `ARNES_PROVIDER` | `anthropic` (default) `\|` `deepseek` `\|` `kimi` `\|` `openai` |
| `ARNES_MODEL` | override del modelo |
| `ARNES_MODE` | modo de permisos al arrancar: `normal` (default) `\|` `auto` `\|` `plan` |
| `ARNES_MD_STYLE` | estilo glamour del markdown (`dark` default; `light`, etc). `auto` se ignora: consulta el terminal y ensucia el input |
| `ARNES_UI` | `tui` (default) `\|` `plain` |
| `ARNES_STREAM` | `off` para desactivar el streaming en la TUI |
| `ARNES_MOUSE` | `on` para capturar el mouse (rueda de scroll; deshabilita la selección de texto del terminal). Por defecto OFF; `Ctrl+O` lo alterna en vivo |
| `ARNES_COMPACT` | `sliding` (default) `\|` `summarize` `\|` `off` |
| `ARNES_COMPACT_AT` | umbral de tokens para auto-compactar (default 120000) |
| `ARNES_MAX_STEPS` | round-trips de herramientas por turno (default 50) |
| `ARNES_MAX_TOKENS` | tope de tokens de salida por llamada al modelo (default 8192) |
| `ARNES_CHECK_CMD` | comando de verificación del proyecto (ej. `go build ./... && go test ./...`); si un turno editó algo, el modelo no puede cerrarlo hasta que pase. Vacío lo desactiva; también se puede fijar en `check_command` en la config |
| `ARNES_PROVIDER_RETRIES` | reintentos extra ante un fallo transitorio del modelo — 429/5xx, stream cortado o estancado — con backoff (default 3; `0` lo desactiva) |
| `ARNES_RESUME` | id (o prefijo) de sesión a reanudar al arrancar |
| `ARNES_RULES` | ruta a un archivo de reglas del proyecto (default: `AGENTS.md` en el cwd) |
| `ARNES_AUTO_UPDATE` | `on` para que el chequeo diario instale la versión nueva por su cuenta |
| `ARNES_CONFIG` / `ARNES_THEME` / `ARNES_MCP` / `ARNES_SUBAGENTS` / `ARNES_HOOKS` / `ARNES_LSP` / `ARNES_SKILLS` | rutas alternativas |
| `ANTHROPIC_API_KEY` / `DEEPSEEK_API_KEY` / `MOONSHOT_API_KEY` / `OPENAI_API_KEY` | API keys por entorno |

### Comandos (slash)

`/help` `/connect` `/mode` `/cost` `/model` `/sessions` `/resume` `/new`
`/compact` `/subagents` `/update-arnes` `/exit`. Escribí `/` en la TUI para el
autocompletado.

### Teclas (TUI)

`Enter` enviar · `Ctrl+C` cancelar el turno en curso (o limpiar el input) ·
`Esc` dos veces para salir · `shift+tab` ciclar modo · `Ctrl+O` alternar captura
de mouse (rueda de scroll ⇄ selección de texto) ·
`↑↓` recuperar mensajes previos (estando al fondo con el input vacío) o scroll
del transcript si ya venís leyendo hacia arriba ·
`PgUp/PgDn` `Ctrl+U/Ctrl+D` `Home/End` scroll.

## Estructura

| Paquete | Responsabilidad |
|---|---|
| `cmd/arnes` | wiring: config, proveedor, herramientas, modos |
| `internal/agent` | el bucle del agente (REPL externo / tool loop interno) |
| `internal/provider` | puerto `Provider` + adaptadores (Anthropic SDK, HTTP OpenAI-compatible) |
| `internal/tool` | herramientas base + `Registry` |
| `internal/approval` | gateway de aprobación (prompt, canal async, allow-all, read-only) |
| `internal/session` | persistencia de conversaciones (`FileStore`, decorator `Persisting`) |
| `internal/memory` | memoria persistente por proyecto (`remember` / `recall`, digest al prompt) |
| `internal/compact` | estrategias de compactación de contexto |
| `internal/subagent` | definiciones de subagentes + `DelegateTool` |
| `internal/skill` | carga de `SKILL.md` (frontmatter + body) + `Registry` |
| `internal/mcp` | cliente MCP stdio (JSON-RPC 2.0) |
| `internal/hook` | hooks pre/post tool call (`hooks.json`) |
| `internal/todo` | checklist de la tarea actual (store + tool `todo_write`) |
| `internal/checkpoint` | puntos de restauración por turno para `/rewind` |
| `internal/lsp` | cliente LSP mínimo (framing Content-Length, un server por extensión) |
| `internal/command` | dispatcher de slash commands (compartido TUI / REPL) |
| `internal/update` | autoactualización: chequeo de releases en GitHub + reemplazo del binario |
| `internal/tui` | front-end Bubble Tea |
| `internal/repl` | front-end de línea (fallback) |

## Desarrollo

```bash
make test    # go test ./...
make vet     # go vet ./...
```

Go 1.26. TDD: cada paquete tiene sus tests, `go test ./...` corre offline
(los providers reales se testean con mocks / `httptest`).
