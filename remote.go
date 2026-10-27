package cascadingconfig

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// resolveRemote fetches the remote file that raw names, or that raw, a plain path in the remote file from, names
// relative to it (DL-204), and returns it as an entry to walk: it finds the reference's resolver, checks it, gets the
// content from the cache or the resolver, and checks the content.
func (p *processor) resolveRemote(from, root *entry, raw string, ancestors []*entry) *entry {
	r := p.newResolving(from, root, raw)
	resolver, rr, failed := r.targetRemote()
	if failed != nil {
		return failed
	}
	if stop := r.checkRemote(rr, ancestors); stop != nil {
		return stop
	}
	got, content, failed := r.obtainRemote(resolver, rr)
	if failed != nil {
		return failed
	}
	return r.acceptRemote(resolver, rr, got, content)
}

// targetRemote works out the reference to fetch, joining a path inside a remote file to its origin, and the resolver
// for its scheme.
func (r *resolving) targetRemote() (IncludeResolver, remoteRef, *entry) {
	c, raw := r.p.ld.c, r.raw
	target, scheme := raw, ""
	if s, remote := remoteScheme(raw); remote {
		scheme = s
	} else {
		joined, err := joinRemote(r.from.remote, raw)
		if err != nil {
			return nil, remoteRef{}, r.badRef(err)
		}
		target, scheme = joined, r.from.remote.ref.Scheme
		r.l.Path = joined
	}
	resolver, ok := c.resolvers[scheme]
	if !ok {
		err := &UnsupportedSchemeError{BaseError: r.about(fmt.Errorf("%q: no resolver is registered for the scheme %q", raw, scheme))}
		return nil, remoteRef{}, r.fail(err, "")
	}
	rr, err := parseRemote(target, scheme, c.name)
	if err != nil {
		return nil, remoteRef{}, r.badRef(err)
	}
	rr.ref.From = r.from.layer
	r.l.Remote = &Remote{Scheme: scheme, Ref: rr.ref.Ref, Pinned: rr.pinned}
	return resolver, rr, nil
}

// checkRemote applies the policy, pinning and cycle checks before anything is fetched. It returns the entry to use
// instead of fetching: a failure, or a duplicate of a file already walked. Otherwise it records the file as walked.
func (r *resolving) checkRemote(rr remoteRef, ancestors []*entry) *entry {
	p, ld := r.p, r.p.ld
	if why, reason := ld.remoteAllowed(r.pol, r.name, rr); why != "" {
		return r.reject(why, reason)
	}
	if r.pol.pinned && !rr.pinned {
		return r.reject(fmt.Sprintf("isn't pinned, and the %s policy requires pinned references", r.name), "not pinned")
	}
	for _, a := range ancestors {
		if a.remote != nil && a.remote.ref.Ref == rr.ref.Ref {
			err := &CycleError{BaseError: r.about(fmt.Errorf("%q includes %s, which is already in this chain of includes", r.raw, rr.ref.Ref))}
			return r.fail(err, "an include cycle")
		}
	}
	if keeper, seen := p.remoteSeen[rr.ref.Ref]; seen {
		r.l.Status = LayerDuplicate
		r.l.Reason = fmt.Sprintf("loaded at a higher position through the %s scope", keeper)
		return &entry{layer: r.l, included: true, remote: &rr}
	}
	if rr.form != formURL {
		if _, ok := ld.remoteFormat(rr, ""); !ok {
			return r.fail(&UnsupportedFormatError{BaseError: r.inside(fmt.Errorf("no format rule or registered decoder's extension matches %s", rr.file))}, "")
		}
	}
	if p.remoteSeen == nil {
		p.remoteSeen = map[string]ScopeName{}
	}
	p.remoteSeen[rr.ref.Ref] = r.l.Scope
	return nil
}

// obtainRemote gets the content: from the cache within its TTL, or pinned; otherwise from the resolver, revalidating
// a cached copy, and falling back to it when fetching fails (DL-207, DL-229). It records where the content came from in
// the layer's Remote.Cache.
func (r *resolving) obtainRemote(resolver IncludeResolver, rr remoteRef) (cacheEntry, []byte, *entry) {
	ld, c, l := r.p.ld, r.p.ld.c, &r.l
	cached, hit := ld.cacheLookup(rr.ref.Ref)
	if hit && (rr.pinned || c.now().Sub(cached.entry.Fetched) < c.cacheTTL) {
		l.Remote.Cache = CacheCached
		return cached.entry, cached.content, nil
	}
	validator := ""
	if hit {
		validator = cached.entry.Validator
	}
	fe, err := ld.fetch(resolver, rr, r.from, r.pol, r.name, validator)
	var fetchErr *FetchError
	switch {
	case err == nil:
	case validator != "" && errors.Is(err, ErrNotModified):
		l.Remote.Cache = CacheRevalidated
		got := cached.entry
		got.Fetched = c.now()
		return got, cached.content, nil
	case hit && errors.As(err, &fetchErr):
		l.Remote.Cache = CacheStale
		l.warn(WarnStaleInclude, "%s: using a cached copy from %s, because fetching it failed: %v", l.Path,
			cached.entry.Fetched.Format("2006-01-02 15:04:05Z07:00"), err)
		return cached.entry, cached.content, nil
	default:
		if cerr := ld.ctx.Err(); cerr != nil {
			return cacheEntry{}, nil, r.fail(contextError(cerr), "")
		}
		return cacheEntry{}, nil, r.fail(err, "")
	}
	if err := checkIdentity(fe.Identity); err != nil {
		return cacheEntry{}, nil, r.fail(&FetchError{BaseError: r.about(fmt.Errorf("%q: the resolver returned %w", r.raw, err))}, "")
	}
	got := cacheEntry{
		Identity: fe.Identity, Sum: sha256Hex(fe.Content), MediaType: fe.MediaType, Fetched: c.now(), Pinned: rr.pinned,
		Validator: keptValidator(fe.Validator),
	}
	if got.Identity == "" {
		got.Identity = "sha256:" + got.Sum
	}
	return got, fe.Content, nil
}

// acceptRemote checks the content, whether fetched or cached: its size, its integrity fragment and its format. It
// stores fresh content in the cache, and records the remote for a watcher's polling.
func (r *resolving) acceptRemote(resolver IncludeResolver, rr remoteRef, got cacheEntry, content []byte) *entry {
	ld, c, l := r.p.ld, r.p.ld.c, &r.l
	l.Remote.Identity = got.Identity
	if int64(len(content)) > c.maxIncludeSize {
		return r.fail(&TooLargeError{BaseError: r.inside(fmt.Errorf("the content is %d bytes, and the limit is %d", len(content), c.maxIncludeSize))}, "")
	}
	if rr.sum != "" && sha256Hex(content) != rr.sum {
		return r.reject("doesn't match its integrity fragment: the content's SHA-256 is "+sha256Hex(content), "the content doesn't match its integrity fragment")
	}
	format, ok := ld.remoteFormat(rr, got.MediaType)
	if !ok {
		return r.fail(&UnsupportedFormatError{BaseError: r.inside(fmt.Errorf("no format rule, extension or media type (%q) has a registered decoder", got.MediaType))}, "")
	}
	l.Format = format
	if l.Remote.Cache == CacheFetched || l.Remote.Cache == CacheRevalidated {
		ld.cacheStore(rr.ref.Ref, got, content)
	}
	if ld.rec != nil {
		ld.rec.remotes = append(ld.rec.remotes, remoteUse{resolver: resolver, rr: rr, from: r.from, pol: r.pol, name: r.name})
	}
	return &entry{layer: *l, data: content, included: true, remote: &rr}
}

// remoteAllowed checks a remote reference's scheme and allow-lists under pol, and says why it's rejected, if it is.
func (ld *loading) remoteAllowed(pol *policy, name string, rr remoteRef) (why, reason string) {
	c := ld.c
	if !pol.usable(c, rr.ref.Scheme) {
		return fmt.Sprintf("can't be included: the %s policy doesn't allow the scheme %q", name, rr.ref.Scheme),
			fmt.Sprintf("the scheme %q isn't allowed", rr.ref.Scheme)
	}
	if !allowed(pol.allow, rr.ref, c.name) || !allowed(pol.allowFor[rr.ref.Scheme], rr.ref, c.name) {
		return fmt.Sprintf("(%s) isn't on the %s policy's allow-list", rr.ref.Ref, name), "not on its allow-list"
	}
	return "", ""
}

// remoteFormat picks a remote file's format: a WithFileFormat rule, matched as allow-lists match the reference, then
// the extension of its file path, then its media type (DL-198).
func (ld *loading) remoteFormat(rr remoteRef, mediaType string) (string, bool) {
	c := ld.c
	if f, ok := c.refRuleFormat(rr.ref); ok {
		return f, true
	}
	if f, ok := c.extFormat(rr.file); ok {
		return f, true
	}
	return c.mediaFormat(mediaType)
}

// fetch calls the resolver for rr with the include timeout, and turns its error into the one the load reports
// (DL-199): an error with a cascadingconfig code keeps its type, and any other is a *FetchError. pol is the policy that
// judged rr, which IncludeRef.Check applies to other references. With a validator, a resolver's ErrNotModified comes
// back as it is (DL-229); without one, it's a *FetchError like any other error.
func (ld *loading) fetch(r IncludeResolver, rr remoteRef, from *entry, pol *policy, name, validator string) (*Fetched, error) {
	c := ld.c
	ref := rr.ref
	ref.MaxSize = c.maxIncludeSize
	ref.Validator = validator
	ref.Check = func(target string) error {
		scheme, ok := remoteScheme(target)
		if !ok {
			return &ReferenceError{BaseError: includeAbout(from, fmt.Errorf("%q isn't a remote reference", target))}
		}
		t, err := parseRemote(target, scheme, c.name)
		if err != nil {
			return &ReferenceError{BaseError: includeAbout(from, fmt.Errorf("%q: %w", target, err))}
		}
		t.ref.From = from.layer
		if why, _ := ld.remoteAllowed(pol, name, t); why != "" {
			return &PolicyError{BaseError: includeAbout(from, fmt.Errorf("%q %s", target, why))}
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(ld.ctx, c.includeTimeout)
	defer cancel()
	fe, err := r.Resolve(ctx, ref)
	switch {
	case err == nil && fe == nil:
		err = errors.New("the resolver returned nothing")
	case err == nil:
		return fe, nil
	case validator != "" && errors.Is(err, ErrNotModified):
		return nil, ErrNotModified
	case CodeOf(err) != 0:
		return nil, err
	}
	var t interface{ Timeout() bool }
	timeout := errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &t) && t.Timeout())
	return nil, &FetchError{BaseError: includeAbout(from, fmt.Errorf("%q: %w", rr.ref.Raw, err)), Timeout: timeout}
}

// remoteUse is a remote include a watched load used, with what polling needs to check it again (DL-230).
type remoteUse struct {
	resolver IncludeResolver
	rr       remoteRef
	from     *entry
	pol      *policy
	name     string
}

// keptValidator is v if the loader may keep it: printable ASCII and at most 1 KiB, so a hostile server can't inject
// headers through it; otherwise none (DL-229).
func keptValidator(v string) string {
	if len(v) > 1024 || strings.ContainsFunc(v, func(r rune) bool { return r < 0x20 || r > 0x7e }) {
		return ""
	}
	return v
}

// checkIdentity rejects an identity that a report can't show safely (DL-198).
func checkIdentity(id string) error {
	if len(id) > 256 {
		return fmt.Errorf("an identity of %d bytes, over the limit of 256", len(id))
	}
	if strings.ContainsFunc(id, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return errors.New("an identity with a control character")
	}
	return nil
}

func sha256Hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}
