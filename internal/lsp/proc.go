package lsp

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// spawn starts the language server and returns its process handle plus its
// stdin (writer) and stdout (reader). stderr is passed through to ours.
func spawn(ctx context.Context, command string, args []string) (io.Closer, io.Writer, io.Reader, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Stderr = os.Stderr
	cmd.WaitDelay = 2 * time.Second

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, nil, err
	}
	return &procCloser{cmd: cmd, stdin: stdin}, stdin, stdout, nil
}

type procCloser struct {
	cmd   *exec.Cmd
	stdin io.Closer
}

func (p *procCloser) Close() error {
	_ = p.stdin.Close()
	_ = p.cmd.Process.Kill()
	return p.cmd.Wait()
}

// pathToURI turns an absolute filesystem path into a file:// URI.
func pathToURI(p string) string {
	p = filepath.ToSlash(p)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p // Windows drive paths
	}
	return "file://" + p
}

// URIPath is the inverse of pathToURI for server-returned locations.
func URIPath(uri string) string {
	s := strings.TrimPrefix(uri, "file://")
	if len(s) > 2 && s[0] == '/' && s[2] == ':' {
		s = s[1:] // Windows: /C:/... -> C:/...
	}
	return filepath.FromSlash(s)
}

// languageID maps a file extension to an LSP languageId.
func languageID(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "go"
	case ".ts":
		return "typescript"
	case ".tsx":
		return "typescriptreact"
	case ".js":
		return "javascript"
	case ".jsx":
		return "javascriptreact"
	case ".py":
		return "python"
	case ".rs":
		return "rust"
	case ".c":
		return "c"
	case ".cc", ".cpp", ".cxx":
		return "cpp"
	case ".java":
		return "java"
	case ".rb":
		return "ruby"
	default:
		return strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	}
}

// SeverityName renders an LSP diagnostic severity.
func SeverityName(s int) string {
	switch s {
	case 1:
		return "error"
	case 2:
		return "warning"
	case 3:
		return "info"
	case 4:
		return "hint"
	default:
		return "diag"
	}
}
