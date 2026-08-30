// Package checkpoint records restore points so the user can rewind a session:
// before each turn it snapshots the conversation history, and as the turn's
// write_file / edit_file calls run it snapshots each touched file's prior
// contents. A rewind restores both.
//
// Files a bash command changes can't be enumerated ahead of time, so when a
// turn runs bash inside a git repository the checkpoint also records a git
// baseline (a `git stash create` commit, or HEAD when the tree was clean). A
// rewind that spans such a turn resets tracked files to that baseline with
// `git checkout` -- which also discards any *other* uncommitted change to a
// tracked file, so it is a coarse undo, matched to what rewind already means.
// Newly created files are left in place.
//
// Checkpoints live in memory for the process lifetime only -- rewind is a
// within-session undo, not persistence.
package checkpoint

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/MauricioJC3/arnes_ng/internal/provider"
)

// maxFileBytes caps the size of a file snapshot; larger files are skipped (the
// rewind will leave them as-is).
const maxFileBytes = 5 << 20

// keep bounds how many checkpoints are retained (oldest dropped first).
const keep = 30

// fileSnap is a file's state at checkpoint time.
type fileSnap struct {
	existed bool
	content []byte
	mode    os.FileMode
}

// gitBaseline is the working-tree state a turn started from, recorded the first
// time that turn runs bash inside a git repo. ref is a commit-ish to restore
// tracked files to: a `git stash create` object, or HEAD when the tree was clean.
type gitBaseline struct {
	ref string
}

// Checkpoint is one restore point: the history before a turn plus the files that
// turn went on to change (and, if it ran bash, the pre-turn git baseline).
type Checkpoint struct {
	Index int
	Label string
	At    time.Time

	history []provider.Message
	files   map[string]fileSnap
	git     *gitBaseline
}

// Files reports how many files this checkpoint captured.
func (c *Checkpoint) Files() int { return len(c.files) }

// History returns the conversation history to restore for this checkpoint.
func (c *Checkpoint) History() []provider.Message {
	return append([]provider.Message(nil), c.history...)
}

// Store holds the ordered checkpoints for one session.
type Store struct {
	mu      sync.Mutex
	list    []*Checkpoint
	next    int
	workdir string // directory git commands run in; "" means the process cwd
}

// Option configures a Store at construction.
type Option func(*Store)

// WithWorkdir sets the directory the git baseline commands run in. The default
// (empty) uses the process working directory, which is what the running agent
// operates in.
func WithWorkdir(dir string) Option { return func(s *Store) { s.workdir = dir } }

// NewStore returns an empty store.
func NewStore(opts ...Option) *Store {
	s := &Store{next: 1}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Begin opens a new checkpoint capturing history (the state before the turn).
// label is a short description, typically the user's message.
func (s *Store) Begin(history []provider.Message, label string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cp := &Checkpoint{
		Index:   s.next,
		Label:   oneLine(label, 60),
		At:      time.Now(),
		history: append([]provider.Message(nil), history...),
		files:   map[string]fileSnap{},
	}
	s.next++
	s.list = append(s.list, cp)
	if len(s.list) > keep {
		s.list = s.list[len(s.list)-keep:]
	}
}

// Observe is the agent tool observer, run before each tool executes: for
// write_file / edit_file it snapshots the target file's current contents into
// the open checkpoint (once per path); for bash it records the git baseline
// (once per turn) so a rewind can undo changes it can't enumerate.
func (s *Store) Observe(call provider.ToolCall) {
	if call.Name == "bash" {
		s.observeGitBaseline()
		return
	}
	if call.Name != "write_file" && call.Name != "edit_file" {
		return
	}
	var in struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(call.Input, &in); err != nil || in.Path == "" {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.list) == 0 {
		return
	}
	cp := s.list[len(s.list)-1]
	if _, done := cp.files[in.Path]; done {
		return
	}

	info, statErr := os.Stat(in.Path)
	if os.IsNotExist(statErr) {
		cp.files[in.Path] = fileSnap{existed: false}
		return
	}
	if statErr != nil || info.IsDir() || info.Size() > maxFileBytes {
		return
	}
	data, err := os.ReadFile(in.Path)
	if err != nil {
		return
	}
	cp.files[in.Path] = fileSnap{existed: true, content: data, mode: info.Mode().Perm()}
}

// observeGitBaseline records the pre-turn working-tree state the first time a
// turn runs bash, so Rewind can reset tracked files even though a shell command
// changed files it never named. A no-op outside a git repo.
func (s *Store) observeGitBaseline() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.list) == 0 {
		return
	}
	cp := s.list[len(s.list)-1]
	if cp.git != nil {
		return // the first bash of the turn already captured the baseline
	}
	if !s.gitOK() {
		return
	}
	ref := s.git("stash", "create")
	if ref == "" {
		ref = s.git("rev-parse", "HEAD") // clean tree: HEAD is the restore point
	}
	if ref != "" {
		cp.git = &gitBaseline{ref: ref}
	}
}

// git runs `git -C <workdir> <args...>` and returns trimmed stdout, or "" on any
// error.
func (s *Store) git(args ...string) string {
	dir := s.workdir
	if dir == "" {
		dir = "."
	}
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// gitOK reports whether workdir is inside a git work tree.
func (s *Store) gitOK() bool {
	return s.git("rev-parse", "--is-inside-work-tree") == "true"
}

// restoreGitBaseline resets tracked files under the repo to b.ref. It also
// reverts other uncommitted tracked changes -- the coarse-undo caveat in the
// package doc.
func (s *Store) restoreGitBaseline(b *gitBaseline) error {
	dir := s.workdir
	if dir == "" {
		dir = "."
	}
	top := s.git("rev-parse", "--show-toplevel")
	if top == "" {
		top = dir
	}
	cmd := exec.Command("git", "-C", top, "checkout", b.ref, "--", ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git checkout %s: %v: %s", b.ref[:min(len(b.ref), 8)], err, strings.TrimSpace(string(out)))
	}
	return nil
}

// List returns the checkpoints, oldest first.
func (s *Store) List() []*Checkpoint {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*Checkpoint(nil), s.list...)
}

// Rewind restores checkpoint index: every captured file is written back (or
// deleted if it did not exist then), and that checkpoint plus every later one is
// dropped. It returns the checkpoint so the caller can rebuild the agent from
// its history.
func (s *Store) Rewind(index int) (*Checkpoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	pos := -1
	for i, cp := range s.list {
		if cp.Index == index {
			pos = i
			break
		}
	}
	if pos < 0 {
		return nil, fmt.Errorf("no hay checkpoint %d", index)
	}
	cp := s.list[pos]

	var failed []string
	// Reset tracked files to the pre-turn git baseline first (a turn that ran
	// bash), then let the precise per-file snapshots override -- they also handle
	// deleting a file the turn newly created.
	if cp.git != nil {
		if err := s.restoreGitBaseline(cp.git); err != nil {
			failed = append(failed, fmt.Sprintf("git baseline (%v)", err))
		}
	}
	for path, snap := range cp.files {
		if err := restore(path, snap); err != nil {
			failed = append(failed, fmt.Sprintf("%s (%v)", path, err))
		}
	}

	s.list = s.list[:pos] // drop this checkpoint and all later ones
	s.next = index        // the next turn re-takes this slot
	if len(failed) > 0 {
		return cp, fmt.Errorf("no se pudieron restaurar %d archivo(s): %s", len(failed), strings.Join(failed, "; "))
	}
	return cp, nil
}

func restore(path string, snap fileSnap) error {
	if !snap.existed {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	mode := snap.mode
	if mode == 0 {
		mode = 0o644
	}
	return os.WriteFile(path, snap.content, mode)
}

func oneLine(s string, max int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	r := []rune(s)
	if max > 1 && len(r) > max {
		return string(r[:max-1]) + "…"
	}
	return s
}

// Summary is a one-line-per-checkpoint listing for the /rewind command.
func (s *Store) Summary() string {
	list := s.List()
	if len(list) == 0 {
		return "no hay checkpoints todavía"
	}
	var b strings.Builder
	for i, cp := range list {
		if i > 0 {
			b.WriteByte('\n')
		}
		label := cp.Label
		if label == "" {
			label = "(sin etiqueta)"
		}
		gitMark := ""
		if cp.git != nil {
			gitMark = " +git"
		}
		fmt.Fprintf(&b, "  %d  %s  %d archivo(s)%s  %s",
			cp.Index, cp.At.Format("15:04:05"), len(cp.files), gitMark, label)
	}
	return b.String()
}
