package session

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// newID builds a sortable id: YYYYMMDD-HHMMSS-xxxx, where xxxx is two random
// bytes in hex to avoid collisions within the same second.
func newID(t time.Time) string {
	var b [2]byte
	_, _ = rand.Read(b[:])
	return t.Format("20060102-150405") + "-" + hex.EncodeToString(b[:])
}
