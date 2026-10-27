package cascadingconfig_test

import (
	"testing"
	"time"

	cc "github.com/kittomic/cascadingconfig"
)

// DL-41, DL-228: one save gives one reload, once the quiet period has passed and not before.
func TestDebounceOneSave(t *testing.T) {
	t.Parallel()
	e := newWatchEnv(t, map[string]string{rootCfg: `{"a": 1}`})
	w := e.watch(nil)
	e.wantLoads(1, "after Watch")
	e.save(rootCfg, `{"a": 2}`)
	e.clock.Advance(quiet - time.Millisecond)
	e.wantLoads(1, "within the quiet period")
	e.clock.Advance(time.Millisecond)
	e.wantLoads(2, "after the quiet period")
	wantValue(t, w.Current(), "a", int64(2))
	e.clock.Advance(10 * time.Second)
	e.wantLoads(2, "long after")
}

// DL-228: each event restarts the quiet period, so a burst with gaps shorter than it reloads once, after its last event.
func TestDebounceBurst(t *testing.T) {
	t.Parallel()
	e := newWatchEnv(t, map[string]string{rootCfg: `{"a": 0}`})
	e.watch(nil)
	for i := range 5 {
		e.save(rootCfg, `{"a": `+string(rune('1'+i))+`}`)
		e.clock.Advance(quiet / 2)
	}
	e.wantLoads(1, "during the burst")
	e.clock.Advance(quiet / 2)
	e.wantLoads(2, "a quiet period after the last event")
	wantValue(t, e.last().New, "a", int64(5))
}

// DL-228: a burst that never goes quiet still reloads maxWait after its first event, and later events start a new
// burst.
func TestDebounceMaxWait(t *testing.T) {
	t.Parallel()
	e := newWatchEnv(t, map[string]string{rootCfg: `{"a": 0}`})
	e.watch(nil)
	step := quiet / 2
	start := e.clock.Now()
	for i := 1; e.clock.Now().Sub(start) < maxWait; i++ {
		e.save(rootCfg, `{"a": 1}`)
		e.clock.Advance(step)
		if got := e.loads.Load(); got > 1 && e.clock.Now().Sub(start) < maxWait {
			t.Fatalf("reloaded after %v, before maxWait", e.clock.Now().Sub(start))
		}
	}
	e.wantLoads(2, "maxWait after the first event")
	e.save(rootCfg, `{"a": 2}`)
	e.settle()
	e.wantLoads(3, "a new burst")
}

// DL-228: WithDebounce sets both durations.
func TestDebounceOption(t *testing.T) {
	t.Parallel()
	e := newWatchEnv(t, map[string]string{rootCfg: `{"a": 1}`})
	e.watch([]cc.WatchOption{cc.WithDebounce(time.Second, 5*time.Second)})
	e.save(rootCfg, `{"a": 2}`)
	e.clock.Advance(quiet)
	e.wantLoads(1, "after the default quiet period")
	e.clock.Advance(time.Second - quiet)
	e.wantLoads(2, "after the configured quiet period")

	for range 8 {
		e.save(rootCfg, `{"a": 3}`)
		e.clock.Advance(700 * time.Millisecond)
	}
	e.wantLoads(3, "a burst of 5.6 s with the configured maxWait of 5 s")
}

// DL-228: a quiet period of 0 reloads on every event, before Send returns.
func TestDebounceZero(t *testing.T) {
	t.Parallel()
	e := newWatchEnv(t, map[string]string{rootCfg: `{"a": 1}`})
	e.watch([]cc.WatchOption{cc.WithDebounce(0, 0)})
	e.save(rootCfg, `{"a": 2}`)
	e.wantLoads(2, "the first event")
	e.save(rootCfg, `{"a": 3}`)
	e.wantLoads(3, "the second event")
}

// DL-226, DL-228, DL-230: invalid watch options are *OptionErrors from Watch, which then returns no watcher.
func TestWatchOptionErrors(t *testing.T) {
	t.Parallel()
	for name, opt := range map[string]cc.WatchOption{
		"negative quiet":           cc.WithDebounce(-time.Millisecond, time.Second),
		"maxWait below quiet":      cc.WithDebounce(time.Second, 500*time.Millisecond),
		"zero poll interval":       cc.WithPollInterval(0),
		"negative poll interval":   cc.WithPollInterval(-time.Second),
		"negative remote interval": cc.WithRemotePoll(-time.Minute),
	} {
		t.Run(name, func(t *testing.T) {
			e := newWatchEnv(t, map[string]string{rootCfg: `{}`})
			w, err := e.loader().Watch(t.Context(), nil, opt)
			if w != nil {
				_ = w.Close()
				t.Errorf("Watch returned a watcher")
			}
			wantErr[*cc.OptionError](t, err, cc.CodeBadRequest, "")
		})
	}
}

// DL-225: a nil notifier factory is an *OptionError from New, naming the option.
func TestWithNotifierNil(t *testing.T) {
	t.Parallel()
	e := newWatchEnv(t, nil)
	_, err := cc.New("myapp", append(e.options(), cc.WithFileSystem(e.fs), cc.WithDecoder(jsonDecoder()), cc.WithNotifier(nil))...)
	wantErr[*cc.OptionError](t, err, cc.CodeBadRequest, "", "WithNotifier")
}
