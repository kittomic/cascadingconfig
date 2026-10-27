package cascadingconfig

import (
	"errors"
	"io/fs"
	"math/rand/v2"
	"time"

	"github.com/kittomic/cascadingconfig/internal/hooks"
)

// remoteState is one unpinned remote include that polls (DL-230).
type remoteState struct {
	use   remoteUse
	every time.Duration
	fails int // failed polls in a row, which back off the interval
	timer hooks.Timer
}

// syncRemotes makes the remote includes rec used the ones that poll: new ones start, gone ones stop, and the rest
// keep their schedule. The caller holds mu.
func (w *Watcher[T]) syncRemotes(rec *watchRecord) {
	seen := map[string]bool{}
	for _, u := range rec.remotes {
		ref := u.rr.ref.Ref
		if u.rr.pinned || seen[ref] {
			continue
		}
		every := w.s.remoteInterval(u.rr.ref, w.c.name)
		if every <= 0 {
			continue
		}
		seen[ref] = true
		if st, ok := w.remotes[ref]; ok {
			st.use = u
			if st.every != every {
				st.every, st.fails = every, 0
				st.timer.Stop()
				w.armRemote(st)
			}
			continue
		}
		st := &remoteState{use: u, every: every}
		w.remotes[ref] = st
		w.armRemote(st)
	}
	for ref, st := range w.remotes {
		if !seen[ref] {
			st.timer.Stop()
			delete(w.remotes, ref)
		}
	}
}

// armRemote sets st's next poll: its interval, doubled for each failed poll in a row up to 8 times, and jittered by up
// to 10% either way unless cctest's clock turned jitter off. The caller holds mu.
func (w *Watcher[T]) armRemote(st *remoteState) {
	d := st.every << min(st.fails, 3)
	if !w.c.noJitter && d >= 10 {
		// jitter only spreads polls out, so it needs no cryptographic randomness
		d += time.Duration(rand.Int64N(int64(d)/5+1)) - d/10 //nolint:gosec // see above
	}
	st.timer = w.c.clock.AfterFunc(d, func() { w.pollRemote(st) })
}

// pollRemote checks st's reference, records an event if it changed, and sets the next poll.
func (w *Watcher[T]) pollRemote(st *remoteState) {
	ref := st.use.rr.ref.Ref
	w.mu.Lock()
	if w.closed || w.remotes[ref] != st {
		w.mu.Unlock()
		return
	}
	use := st.use
	w.mu.Unlock()
	changed, failed := w.checkRemote(use)
	w.mu.Lock()
	if w.closed || w.remotes[ref] != st {
		w.mu.Unlock()
		return
	}
	if failed {
		st.fails++
	} else {
		st.fails = 0
	}
	now := false
	if changed {
		now = w.markPending()
	}
	w.armRemote(st)
	w.mu.Unlock()
	if now {
		w.fire(-1, -1)
	}
}

// checkRemote revalidates use's reference, ignoring the cache TTL (DL-229, DL-230). New content is stored in the
// cache, so the reload it causes reads it from there. It reports whether the reference changed, and whether the check
// failed with a *FetchError, which backs off. Without the cache there's no validator to send, so every poll counts as
// a change and reloads in full; so does any error but a *FetchError, so the reload reports it.
func (w *Watcher[T]) checkRemote(use remoteUse) (changed, failed bool) {
	c := w.c
	if c.noCache {
		return true, false
	}
	ld := &loading{ctx: w.ctx, c: c, roots: map[string]Root{}, dirs: map[string][]fs.DirEntry{}, skipped: map[ScopeName]string{}}
	defer ld.closeRoots()
	ref := use.rr.ref.Ref
	cached, hit := ld.cacheLookup(ref)
	if !hit {
		return true, false
	}
	fe, err := ld.fetch(use.resolver, use.rr, use.from, use.pol, use.name, cached.entry.Validator)
	var fetchErr *FetchError
	switch {
	case err == nil:
	case cached.entry.Validator != "" && errors.Is(err, ErrNotModified):
		e := cached.entry
		e.Fetched = c.now()
		ld.cacheStore(ref, e, cached.content)
		return false, false
	case errors.As(err, &fetchErr):
		return false, true
	default:
		return true, false
	}
	if checkIdentity(fe.Identity) != nil || int64(len(fe.Content)) > c.maxIncludeSize {
		return true, false
	}
	e := cacheEntry{
		Identity: fe.Identity, Sum: sha256Hex(fe.Content), MediaType: fe.MediaType, Fetched: c.now(),
		Validator: keptValidator(fe.Validator),
	}
	if e.Identity == "" {
		e.Identity = "sha256:" + e.Sum
	}
	ld.cacheStore(ref, e, fe.Content)
	return e.Sum != cached.entry.Sum, false
}
