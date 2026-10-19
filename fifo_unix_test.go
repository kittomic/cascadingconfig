//go:build unix

package cascadingconfig_test

import (
	"path/filepath"
	"syscall"
	"testing"
	"time"

	cc "github.com/kittomic/cascadingconfig"
)

// DL-63: on a real filesystem, a FIFO named like a candidate is skipped with a warning, without blocking.
func TestOSFIFODoesNotBlock(t *testing.T) {
	dir := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(dir, "config.json"), 0o644); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	logs := &records{}
	l, err := cc.New("myapp", cc.WithDecoder(jsonDec), cc.WithLogger(slogFor(logs)), useKind(cc.FixedDir(dir, p("config.json")...)))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := l.Load(t.Context())
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if len(logs.got) != 1 || attrs(logs.got[0])["code"] != string(cc.WarnNotAFile) {
			t.Errorf("logs = %v, want one not-a-file warning", logs.got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Load blocked on a FIFO")
	}
}
