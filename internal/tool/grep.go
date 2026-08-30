package tool

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// maxGrepMatches caps how many matching lines grep returns.
const maxGrepMatches = 200

// skipDirs are never descended into by the Go fallback.
var skipDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "dist": true, "build": true,
	".idea": true, ".vscode": true,
}

// Grep searches file contents for a regular expression. It shells out to
// ripgrep when available, otherwise walks the tree itself.
type Grep struct{}

func (Grep) Name() string { return "grep" }

func (Grep) Description() string {
	return "Busca un patrón (regex) en el contenido de los archivos, relativo al directorio " +
		"actual. Devuelve líneas como `archivo:línea: texto`. Filtrá con `path` (dónde buscar) " +
		"y/o `glob` (qué archivos, ej. `*.go`). Esta es la forma de buscar texto en el código; " +
		"no uses bash con grep/rg."
}

func (Grep) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pattern":     map[string]any{"type": "string", "description": "Expresión regular a buscar."},
			"path":        map[string]any{"type": "string", "description": "Directorio o archivo donde buscar (default: todo el proyecto)."},
			"glob":        map[string]any{"type": "string", "description": "Filtro de archivos (ej. `*.go`, `*_test.go`)."},
			"ignore_case": map[string]any{"type": "boolean", "description": "Ignorar mayúsculas/minúsculas."},
		},
		"required": []string{"pattern"},
	}
}

func (Grep) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var in struct {
		Pattern    string `json:"pattern"`
		Path       string `json:"path"`
		Glob       string `json:"glob"`
		IgnoreCase bool   `json:"ignore_case"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return "", err
	}
	if strings.TrimSpace(in.Pattern) == "" {
		return "", errors.New("el parámetro 'pattern' es obligatorio")
	}
	root := in.Path
	if root == "" {
		root = "."
	}

	if _, err := exec.LookPath("rg"); err == nil {
		return grepRipgrep(ctx, in.Pattern, root, in.Glob, in.IgnoreCase)
	}
	return grepWalk(in.Pattern, root, in.Glob, in.IgnoreCase)
}

func grepRipgrep(ctx context.Context, pattern, root, glob string, ic bool) (string, error) {
	args := []string{"--line-number", "--no-heading", "--color", "never", "--max-count", "50"}
	if ic {
		args = append(args, "--ignore-case")
	}
	if glob != "" {
		args = append(args, "--glob", glob)
	}
	args = append(args, "--", pattern, root)

	cmd := exec.CommandContext(ctx, "rg", args...)
	hardenCmd(cmd)
	out, err := cmd.CombinedOutput()
	text := strings.TrimRight(string(out), "\n")
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			if ee.ExitCode() == 1 { // rg convention: no matches
				return "sin coincidencias", nil
			}
			return "", fmt.Errorf("rg (exit %d): %s", ee.ExitCode(), strings.TrimSpace(text))
		}
		return "", fmt.Errorf("rg: %w", err)
	}
	if text == "" {
		return "sin coincidencias", nil
	}
	return clampLines(text, maxGrepMatches), nil
}

func grepWalk(pattern, root, glob string, ic bool) (string, error) {
	flags := ""
	if ic {
		flags = "(?i)"
	}
	re, err := regexp.Compile(flags + pattern)
	if err != nil {
		return "", fmt.Errorf("regex inválida: %w", err)
	}

	var hits []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if glob != "" {
			if ok, _ := filepath.Match(glob, d.Name()); !ok {
				return nil
			}
		}
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()

		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for line := 1; sc.Scan(); line++ {
			if isBinary(sc.Bytes()) {
				return nil
			}
			if re.Match(sc.Bytes()) {
				hits = append(hits, fmt.Sprintf("%s:%d: %s", path, line, strings.TrimSpace(sc.Text())))
				if len(hits) >= maxGrepMatches {
					return errStop
				}
			}
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStop) {
		return "", err
	}
	if len(hits) == 0 {
		return "sin coincidencias", nil
	}
	out := strings.Join(hits, "\n")
	if errors.Is(err, errStop) {
		out += fmt.Sprintf("\n… (cortado en %d)", maxGrepMatches)
	}
	return out, nil
}

var errStop = errors.New("stop")

func isBinary(b []byte) bool {
	for _, c := range b {
		if c == 0 {
			return true
		}
	}
	return false
}

func clampLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[:n], "\n") + fmt.Sprintf("\n… (cortado en %d)", n)
}
