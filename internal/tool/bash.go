package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

// DefaultBashTimeout bounds a single command when Bash.Timeout is zero.
const DefaultBashTimeout = 30 * time.Second

// Bash runs a shell command via `bash -c` and returns its combined output.
// A non-zero exit code is a normal result (reported in the output), not a
// tool failure.
type Bash struct {
	Timeout time.Duration
}

func (Bash) Name() string { return "bash" }

func (Bash) Description() string {
	return "Ejecuta un comando de shell (con `bash -c`) en la máquina local y devuelve " +
		"su salida combinada (stdout + stderr). Usala para inspeccionar el sistema de " +
		"archivos, correr binarios, git, tests, etc. La salida se trunca según el modelo."
}

func (Bash) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "El comando de shell a ejecutar.",
			},
		},
		"required": []string{"command"},
	}
}

func (b Bash) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var in struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return "", err
	}
	if in.Command == "" {
		return "", errors.New("el parámetro 'command' es obligatorio")
	}

	timeout := b.Timeout
	if timeout <= 0 {
		timeout = DefaultBashTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, "bash", "-c", in.Command).CombinedOutput()
	result := string(out)
	if err != nil {
		result += fmt.Sprintf("\n[el comando terminó con error: %v]", err)
	}
	return result, nil
}
