package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// codeGraphTimeout bounds one codegraph invocation.
const codeGraphTimeout = 60 * time.Second

// codeGraphOps is the allowlist of read-only subcommands. Anything that mutates
// the index or the install (init, index, sync, uninit, install, upgrade) is
// refused -- the model uses this to read structure, not to manage the tool.
var codeGraphOps = map[string]bool{
	"explore": true, "query": true, "callers": true, "callees": true,
	"impact": true, "node": true, "files": true, "affected": true, "status": true,
}

// CodeGraph queries a project's codegraph index (a SQLite graph of symbols,
// edges and files) so the model can answer structural questions -- who calls X,
// what X calls, a change's blast radius, where a symbol lives -- in one call
// instead of a grep loop. It only runs read-only subcommands. Build it with
// NewCodeGraph, which returns nil when the CLI or the index is missing so the
// caller can skip registering it.
type CodeGraph struct {
	bin string
	dir string
}

// NewCodeGraph returns a CodeGraph tool when a `codegraph` binary is on PATH and
// dir contains a .codegraph/ index, otherwise nil.
func NewCodeGraph(dir string) *CodeGraph {
	bin, err := exec.LookPath("codegraph")
	if err != nil {
		return nil
	}
	if dir == "" {
		dir, _ = os.Getwd()
	}
	if fi, err := os.Stat(filepath.Join(dir, ".codegraph")); err != nil || !fi.IsDir() {
		return nil
	}
	return &CodeGraph{bin: bin, dir: dir}
}

func (CodeGraph) Name() string { return "code_graph" }

func (CodeGraph) Description() string {
	return "Consulta el grafo de código del proyecto (índice de codegraph: símbolos, " +
		"llamadas, dependencias) para preguntas ESTRUCTURALES -- quién llama a X, a quién " +
		"llama X, el radio de impacto de un cambio, dónde vive un símbolo -- en una sola " +
		"llamada, en vez de encadenar greps. Es de solo lectura. `op`: explore | query | " +
		"callers | callees | impact | node | files | affected | status. `query` es el " +
		"símbolo, patrón o ruta según la op (opcional para 'files' y 'status')."
}

func (CodeGraph) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"op": map[string]any{
				"type":        "string",
				"description": "Subcomando de solo lectura: explore, query, callers, callees, impact, node, files, affected, status.",
			},
			"query": map[string]any{
				"type":        "string",
				"description": "El argumento según la op: nombre de símbolo, patrón de búsqueda o ruta. Opcional para 'files' y 'status'.",
			},
		},
		"required": []string{"op"},
	}
}

func (c CodeGraph) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var in struct {
		Op    string `json:"op"`
		Query string `json:"query"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return "", err
	}
	in.Op = strings.TrimSpace(in.Op)
	if in.Op == "" {
		return "", errors.New("el parámetro 'op' es obligatorio")
	}
	if !codeGraphOps[in.Op] {
		return "", fmt.Errorf("op %q no permitida; codegraph acá es de solo lectura: explore, query, "+
			"callers, callees, impact, node, files, affected, status", in.Op)
	}

	ctx, cancel := context.WithTimeout(ctx, codeGraphTimeout)
	defer cancel()

	args := []string{in.Op}
	if q := strings.TrimSpace(in.Query); q != "" {
		args = append(args, q)
	}
	cmd := exec.CommandContext(ctx, c.bin, args...)
	cmd.Dir = c.dir
	out, err := cmd.CombinedOutput()
	res := string(out)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return res + fmt.Sprintf("\n[codegraph %s se canceló por límite de tiempo (%s)]", in.Op, codeGraphTimeout), nil
		}
		return res + fmt.Sprintf("\n[codegraph %s terminó con error: %v]", in.Op, err), nil
	}
	return res, nil
}
