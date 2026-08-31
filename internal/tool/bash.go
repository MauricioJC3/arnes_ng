package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/MauricioJC3/arnes_ng/internal/shell"
)

// Bash timeout bounds. DefaultBashTimeout applies when a call omits
// `timeout_seconds` and Bash.Timeout is zero. MaxBashTimeout caps whatever the
// caller asks for, so a runaway command can never hang the turn indefinitely.
const (
	DefaultBashTimeout = 2 * time.Minute
	MaxBashTimeout     = 10 * time.Minute
)

// Bash runs a shell command through the platform interpreter (`bash -c` on
// Unix, PowerShell on Windows; overridable with $ARNES_SHELL) and returns its
// combined output. A non-zero exit code is a normal result (reported in the
// output), not a tool failure.
type Bash struct {
	// Timeout is the fallback bound when a call omits `timeout_seconds`.
	// Zero means DefaultBashTimeout.
	Timeout time.Duration
}

func (Bash) Name() string { return "bash" }

func (Bash) Description() string {
	base := "Ejecuta un comando de shell con `" + shell.Label() + "` y devuelve su salida " +
		"combinada (stdout + stderr). Usala para EJECUTAR cosas: tests, build, git, binarios, " +
		"instaladores. Para BUSCAR texto usá grep, y para encontrar archivos usá glob " +
		"(no `grep`/`find`/`rg` por acá). Un exit code distinto de cero no es un error de " +
		"la herramienta: se anexa a la salida y vos decidís cómo seguir. El comando se " +
		"corta a los 120s por defecto; pasá `timeout_seconds` (máx. 600) para builds, " +
		"instalaciones o suites de tests que legítimamente tarden más."
	if !shell.POSIX() {
		base += " OJO: la shell es PowerShell — encadená con `;` (no `&&`), y para escribir " +
			"archivos usá write_file, no redirección `>` (PowerShell 5.1 escribe UTF-16 con BOM)."
	}
	return base
}

func (Bash) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "El comando de shell a ejecutar.",
			},
			"timeout_seconds": map[string]any{
				"type": "integer",
				"description": "Tiempo máximo en segundos antes de matar el comando. " +
					"Opcional; por defecto 120, máximo 600. Subilo para comandos largos " +
					"como `docker compose build` o una suite de tests completa.",
			},
		},
		"required": []string{"command"},
	}
}

func (b Bash) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var in struct {
		Command        string `json:"command"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return "", err
	}
	if in.Command == "" {
		return "", errors.New("el parámetro 'command' es obligatorio")
	}

	timeout := b.resolveTimeout(in.TimeoutSeconds)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := shell.CommandContext(ctx, in.Command)
	hardenCmd(cmd) // own process group + bounded wait, so a cancel actually kills it
	out, err := cmd.CombinedOutput()
	result := string(out)
	if err != nil {
		switch {
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			result += fmt.Sprintf("\n[el comando se canceló por límite de tiempo (%s); "+
				"si es legítimamente largo, reintentá con un timeout_seconds mayor]", timeout)
		case errors.Is(ctx.Err(), context.Canceled):
			result += "\n[el comando se canceló (Ctrl+C)]"
		default:
			result += fmt.Sprintf("\n[el comando terminó con error: %v]", err)
		}
	}
	return result, nil
}

// resolveTimeout picks the effective bound: the per-call value when positive,
// otherwise Bash.Timeout, otherwise DefaultBashTimeout -- clamped to
// MaxBashTimeout.
func (b Bash) resolveTimeout(perCallSeconds int) time.Duration {
	d := b.Timeout
	if d <= 0 {
		d = DefaultBashTimeout
	}
	if perCallSeconds > 0 {
		d = time.Duration(perCallSeconds) * time.Second
	}
	if d > MaxBashTimeout {
		d = MaxBashTimeout
	}
	return d
}
