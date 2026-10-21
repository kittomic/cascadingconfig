package main

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/cctest"
	"github.com/kittomic/cascadingconfig/examples/internal/exampletest"
)

var shipped = []string{"ccx-secrets.toml", "secrets/db.password", "secrets/api.token"}

func TestSecrets(t *testing.T) {
	hidden := map[string]string{
		exampletest.Cwd + "/secrets/.ignored":           "not a setting\n",
		exampletest.Cwd + "/secrets/..data/db.password": "not read: inside a dot directory\n",
	}
	for _, c := range []struct {
		name  string
		args  []string
		extra map[string]string
	}{
		{"shipped", nil, hidden},
		{"absolute", []string{"-secrets", "/run/secrets/ccx-secrets"},
			map[string]string{"/run/secrets/ccx-secrets/db.password": "mounted\n"}},
		{"missing", []string{"-secrets", "/nowhere"}, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, world := exampletest.World(t, shipped, c.extra, nil)
			var out bytes.Buffer
			if err := run(t.Context(), c.args, &out, world); err != nil {
				t.Fatal(err)
			}
			cctest.Golden(t, c.name+".golden", out.Bytes())
		})
	}
}

// syncBuffer is a bytes.Buffer the test can read while run writes to it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// With -watch, rotating a secret reloads: the scope's WatchDirs puts the directory in the watch set.
func TestSecretsRotation(t *testing.T) {
	fsys, world := exampletest.World(t, shipped, nil, nil)
	notifier := cctest.NewNotifier()
	clock := cctest.NewClock(time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	var out syncBuffer
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, []string{"-watch"}, &out, world, cascadingconfig.WithNotifier(notifier.New),
			cctest.WithClock(clock))
	}()
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(out.String(), "token") {
		if time.Now().After(deadline) {
			t.Fatalf("the watch didn't start:\n%s", out.String())
		}
		time.Sleep(time.Millisecond)
	}
	if got := notifier.Watched(); !slices.Contains(got, exampletest.Cwd+"/secrets") {
		t.Errorf("watched %q, without the secrets directory", got)
	}

	file := exampletest.Cwd + "/secrets/db.password"
	fsys.WriteFile(file, []byte("rotated-db-password\n"), 0o644)
	notifier.Send(file)
	clock.Advance(time.Second)

	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	cctest.Golden(t, "rotation.golden", []byte(out.String()))
}
