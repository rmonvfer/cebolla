// Package report keeps the addresses of filter hits for manual reporting to a
// hotline. Each address is encrypted to an age public key whose private half
// lives outside the VM; without a key configured nothing is written at all.
// Page content is never part of a report.
package report

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"filippo.io/age"
)

type Queue struct {
	dir string
	rcp age.Recipient
}

// New returns a queue writing to dir, or nil if recipient is empty.
func New(dir, recipient string) (*Queue, error) {
	if recipient == "" {
		return nil, nil
	}
	r, err := age.ParseX25519Recipient(recipient)
	if err != nil {
		return nil, fmt.Errorf("report recipient: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Queue{dir: dir, rcp: r}, nil
}

// Add writes one encrypted file: "<address>.onion<TAB><time>".
func (q *Queue) Add(addr string, at time.Time) error {
	if q == nil {
		return nil
	}
	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, q.rcp)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "%s.onion\t%s\n", addr, at.UTC().Format(time.RFC3339))
	if err := w.Close(); err != nil {
		return err
	}
	name := filepath.Join(q.dir, fmt.Sprintf("%d.age", at.UnixNano()))
	return os.WriteFile(name, buf.Bytes(), 0o600)
}
