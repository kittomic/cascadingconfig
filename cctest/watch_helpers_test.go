package cctest_test

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/kittomic/cascadingconfig/cctest"
)

var t0 = time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

// handler is how the watcher tells the notifier New returned that it has finished with an event or error.
type handler interface{ Handled() }

// DL-231: timers fire inside Advance, in time order, with the clock at each timer's time, and not before they're due.
func TestClockTimers(t *testing.T) {
	t.Parallel()
	c := cctest.NewClock(t0)
	var fired []string
	at := func(name string) func() {
		return func() { fired = append(fired, name+"@"+c.Now().Sub(t0).String()) }
	}
	cctest.AfterFunc(c, 3*time.Second, at("c"))
	cctest.AfterFunc(c, time.Second, at("a"))
	cctest.AfterFunc(c, 2*time.Second, at("b"))
	cctest.AfterFunc(c, 2*time.Second, at("b2"))
	c.Advance(time.Second - time.Nanosecond)
	if len(fired) != 0 || c.Timers() != 4 {
		t.Fatalf("before any is due: fired %q with %d waiting", fired, c.Timers())
	}
	c.Advance(5 * time.Second)
	if want := []string{"a@1s", "b@2s", "b2@2s", "c@3s"}; !slices.Equal(fired, want) {
		t.Errorf("fired %q, want %q", fired, want)
	}
	if got := c.Now().Sub(t0); got != 6*time.Second-time.Nanosecond {
		t.Errorf("the clock ended at %v, want the target", got)
	}
	if c.Timers() != 0 {
		t.Errorf("%d timers still waiting", c.Timers())
	}
}

// DL-231: a timer can start another, which fires in the same Advance if it falls due; a timer of 0 or less waits for
// the next Advance, Advance(0) included.
func TestClockChainedTimers(t *testing.T) {
	t.Parallel()
	c := cctest.NewClock(t0)
	var fired []time.Duration
	var tick func()
	tick = func() {
		fired = append(fired, c.Now().Sub(t0))
		cctest.AfterFunc(c, time.Second, tick)
	}
	cctest.AfterFunc(c, time.Second, tick)
	c.Advance(3500 * time.Millisecond)
	if want := []time.Duration{time.Second, 2 * time.Second, 3 * time.Second}; !slices.Equal(fired, want) {
		t.Errorf("fired at %v, want %v", fired, want)
	}
	now := false
	cctest.AfterFunc(c, 0, func() { now = true })
	if now {
		t.Fatalf("a timer of 0 fired before Advance")
	}
	c.Advance(0)
	if !now {
		t.Errorf("a timer of 0 didn't fire on Advance(0)")
	}
}

// DL-231: Stop removes a timer and reports whether it hadn't fired; Set forward fires due timers, and Set back fires
// none.
func TestClockStopAndSet(t *testing.T) {
	t.Parallel()
	c := cctest.NewClock(t0)
	fired := 0
	tm := cctest.AfterFunc(c, time.Second, func() { fired++ })
	if !tm.Stop() {
		t.Errorf("Stop of a waiting timer = false")
	}
	if tm.Stop() {
		t.Errorf("a second Stop = true")
	}
	c.Advance(time.Hour)
	if fired != 0 {
		t.Errorf("a stopped timer fired")
	}
	cctest.AfterFunc(c, time.Minute, func() { fired++ })
	c.Set(t0)
	if fired != 0 || !c.Now().Equal(t0) {
		t.Errorf("Set back: fired %d, now %v", fired, c.Now())
	}
	c.Set(t0.Add(2 * time.Hour))
	if fired != 1 {
		t.Errorf("Set forward past the timer: fired %d, want 1", fired)
	}
	done := cctest.AfterFunc(c, time.Second, func() {})
	c.Advance(time.Second)
	if done.Stop() {
		t.Errorf("Stop of a fired timer = true")
	}
}

// DL-231: the fake notifier records watched directories, fails Add where told, and delivers events and errors, with
// Send and SendError waiting for Handled.
func TestNotifier(t *testing.T) {
	t.Parallel()
	n := cctest.NewNotifier()
	nn, err := n.New()
	if err != nil || nn == nil {
		t.Fatalf("New() = %v, %v", nn, err)
	}
	n.FailAdd("/b", errors.New("no"))
	for _, d := range []string{"/c", "/a", "/b"} {
		err := nn.Add(d)
		if (err != nil) != (d == "/b") {
			t.Errorf("Add(%s) = %v", d, err)
		}
	}
	if err := nn.Remove("/c"); err != nil {
		t.Errorf("Remove: %v", err)
	}
	if got := n.Watched(); !slices.Equal(got, []string{"/a"}) {
		t.Errorf("Watched() = %q, want /a", got)
	}

	// a watcher stand-in that takes one event, then one error, and is told when to call Handled for each
	got := make(chan string, 1)
	release := make(chan struct{})
	go func() {
		got <- <-nn.Events()
		<-release
		nn.(handler).Handled()
		got <- (<-nn.Errors()).Error()
		<-release
		nn.(handler).Handled()
	}()
	for _, step := range []struct {
		name, want string
		send       func()
	}{
		{"Send", "/a/x", func() { n.Send("/a/x") }},
		{"SendError", "overflow", func() { n.SendError(errors.New("overflow")) }},
	} {
		done := make(chan struct{})
		go func() {
			defer close(done)
			step.send()
		}()
		if v := <-got; v != step.want {
			t.Errorf("%s delivered %q, want %q", step.name, v, step.want)
		}
		select {
		case <-done:
			t.Fatalf("%s returned before Handled", step.name)
		case <-time.After(50 * time.Millisecond):
		}
		release <- struct{}{}
		<-done
	}
}

// DL-231: Close closes the channels, Send after it returns at once, a second Close is fine, and New opens it again.
func TestNotifierClose(t *testing.T) {
	t.Parallel()
	n := cctest.NewNotifier()
	nn, _ := n.New()
	_ = nn.Add("/a")
	if err := nn.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := nn.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
	if _, ok := <-nn.Events(); ok {
		t.Errorf("Events is still open")
	}
	if _, ok := <-nn.Errors(); ok {
		t.Errorf("Errors is still open")
	}
	n.Send("/a/x")
	n.SendError(errors.New("x"))
	again, _ := n.New()
	if len(n.Watched()) != 0 {
		t.Errorf("reopened with %q watched", n.Watched())
	}
	go func() {
		<-again.Events()
		again.(handler).Handled()
	}()
	n.Send("/b")
}
