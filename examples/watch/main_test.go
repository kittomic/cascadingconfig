package main

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/cctest"
	"github.com/kittomic/cascadingconfig/examples/internal/exampletest"
)

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

// The service reports an edit as its diff, keeps its last good config through a broken edit, recovers, and stops on
// cancellation.
func TestWatch(t *testing.T) {
	fsys, world := exampletest.World(t, []string{"ccx-watch.toml"}, nil, nil)
	notifier := cctest.NewNotifier()
	clock := cctest.NewClock(time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	var out syncBuffer
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, nil, &out, world, cascadingconfig.WithNotifier(notifier.New), cctest.WithClock(clock))
	}()
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(out.String(), "watching") {
		if time.Now().After(deadline) {
			t.Fatalf("the service didn't start:\n%s", out.String())
		}
		time.Sleep(time.Millisecond)
	}

	file := exampletest.Cwd + "/ccx-watch.toml"
	edit := func(content string) {
		fsys.WriteFile(file, []byte(content), 0o644)
		notifier.Send(file)
		clock.Advance(time.Second) // past the debounce; returns once the handler has run
	}
	edit("[server]\nport = 8080\n\n[limits]\nrequests_per_second = 250\n")
	edit("[server]\nport = 8080\n\n[limits]\nrequests_per_second = [\n")
	edit("[server]\nport = 9090\n\n[limits]\nrequests_per_second = 250\nburst = 50\n")

	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	cctest.Golden(t, "watch.golden", []byte(out.String()))
}
