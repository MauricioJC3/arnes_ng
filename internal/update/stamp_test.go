package update

import (
	"path/filepath"
	"testing"
	"time"
)

func TestStamp(t *testing.T) {
	s := Stamp{Path: filepath.Join(t.TempDir(), "last_update_check")}
	now := time.Now()

	if !s.Due(now, 24*time.Hour) {
		t.Fatal("a missing stamp should be due")
	}
	if err := s.Mark(now); err != nil {
		t.Fatal(err)
	}
	if s.Due(now.Add(time.Hour), 24*time.Hour) {
		t.Fatal("checked an hour ago should not be due")
	}
	if !s.Due(now.Add(25*time.Hour), 24*time.Hour) {
		t.Fatal("checked 25h ago should be due")
	}
}
