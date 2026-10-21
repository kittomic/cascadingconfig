package cascadingconfig

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// resolve follows one include reference that from wrote: it applies the include limits (DL-210), then resolves a
// remote reference, or a path inside a remote file, through its resolver, and anything else as a local file.
// ancestors are the chain down to from, the root file first.
func (p *processor) resolve(from *entry, ancestors []*entry, raw string) *entry {
	ld, c := p.ld, p.ld.c
	root := ancestors[0]
	p.refs++
	switch {
	case p.refs > c.maxIncludes:
		err := &TooLargeError{BaseError: includeAbout(from, fmt.Errorf(
			"%q is include reference number %d in this load, over the limit of %d", raw, p.refs, c.maxIncludes))}
		return failInclude(ld.includeLayer(from, root, raw), err, "over the limit of includes in one load")
	case len(ancestors) > c.maxIncludeDepth:
		err := &CycleError{BaseError: includeAbout(from, fmt.Errorf(
			"%q would be nested %d levels deep, over the limit of %d", raw, len(ancestors), c.maxIncludeDepth))}
		return failInclude(ld.includeLayer(from, root, raw), err, "nested too deeply")
	}
	if _, remote := remoteScheme(raw); remote || from.remote != nil {
		return p.resolveRemote(from, root, raw, ancestors)
	}
	return ld.resolveInclude(from, root, raw, ancestors, p.found)
}

// includeLayer is the layer of an include that from wrote, with its metadata from root, the first file of the chain
// (DL-184).
func (ld *loading) includeLayer(from, root *entry, raw string) Layer {
	return Layer{
		Path: raw, Scope: root.layer.Scope, Variant: root.layer.Variant, Env: root.layer.Env, Trust: root.layer.Trust,
		ConfigRoot: root.layer.ConfigRoot, IncludedBy: from.layer.Path, Status: LayerLoaded,
	}
}

// includeAbout is the base of an error about a reference: it names the including file (DL-185).
func includeAbout(from *entry, err error) BaseError {
	return BaseError{Op: "include", Path: from.layer.Path, Scope: string(from.layer.Scope), Err: err}
}

// failInclude is an included layer that failed with err.
func failInclude(l Layer, err error, reason string) *entry {
	if reason == "" {
		reason = failReason(err)
	}
	l.Status, l.Reason = LayerFailed, reason
	return &entry{layer: l, err: err, included: true}
}

// resolveRemote fetches the remote file that raw names, or that raw, a plain path in the remote file from, names
// relative to it (DL-204), and returns it as an entry to walk.
func (p *processor) resolveRemote(from, root *entry, raw string, ancestors []*entry) *entry {
	ld, c := p.ld, p.ld.c
	scope := root.layer.Scope
	pol := c.policyFor(scope)
	name := policyName(scope)
	l := ld.includeLayer(from, root, raw)
	fail := func(err error, reason string) *entry { return failInclude(l, err, reason) }
	about := func(err error) BaseError { return includeAbout(from, err) }
	inside := func(err error) BaseError {
		return BaseError{Op: "include", Path: l.Path, Scope: string(scope), Err: err}
	}
	reject := func(why, reason string) *entry {
		return fail(&PolicyError{BaseError: about(fmt.Errorf("%q %s", raw, why))}, "rejected by the "+name+" policy: "+reason)
	}
	badRef := func(err error) *entry {
		return fail(&ReferenceError{BaseError: about(fmt.Errorf("%q: %w", raw, err))}, "")
	}

	target, scheme := raw, ""
	if s, remote := remoteScheme(raw); remote {
		scheme = s
	} else {
		joined, err := joinRemote(from.remote, raw)
		if err != nil {
			return badRef(err)
		}
		target, scheme = joined, from.remote.ref.Scheme
		l.Path = joined
	}
	resolver, ok := c.resolvers[scheme]
	if !ok {
		return fail(&UnsupportedSchemeError{BaseError: about(fmt.Errorf("%q: no resolver is registered for the scheme %q", raw, scheme))}, "")
	}
	rr, err := parseRemote(target, scheme, c.name)
	if err != nil {
		return badRef(err)
	}
	rr.ref.From = from.layer
	l.Remote = &Remote{Scheme: scheme, Ref: rr.ref.Ref, Pinned: rr.pinned}
	if why, reason := ld.remoteAllowed(pol, name, rr); why != "" {
		return reject(why, reason)
	}
	if pol.pinned && !rr.pinned {
		return reject(fmt.Sprintf("isn't pinned, and the %s policy requires pinned references", name), "not pinned")
	}
	for _, a := range ancestors {
		if a.remote != nil && a.remote.ref.Ref == rr.ref.Ref {
			return fail(&CycleError{BaseError: about(fmt.Errorf("%q includes %s, which is already in this chain of includes", raw, rr.ref.Ref))},
				"an include cycle")
		}
	}
	if keeper, seen := p.remoteSeen[rr.ref.Ref]; seen {
		l.Status = LayerDuplicate
		l.Reason = fmt.Sprintf("loaded at a higher position through the %s scope", keeper)
		return &entry{layer: l, included: true, remote: &rr}
	}
	if rr.form != formURL {
		if _, ok := ld.remoteFormat(rr, ""); !ok {
			return fail(&UnsupportedFormatError{BaseError: inside(fmt.Errorf("no format rule or registered decoder's extension matches %s", rr.file))}, "")
		}
	}
	if p.remoteSeen == nil {
		p.remoteSeen = map[string]ScopeName{}
	}
	p.remoteSeen[rr.ref.Ref] = scope

	cached, hit := ld.cacheLookup(rr.ref.Ref)
	var got cacheEntry
	var content []byte
	switch {
	case hit && (rr.pinned || c.now().Sub(cached.entry.Fetched) < c.cacheTTL):
		got, content, l.Remote.Cache = cached.entry, cached.content, "cached"
	default:
		validator := ""
		if hit {
			validator = cached.entry.Validator
		}
		fe, err := ld.fetch(resolver, rr, from, pol, name, validator)
		var fetchErr *FetchError
		switch {
		case err == nil:
		case validator != "" && errors.Is(err, ErrNotModified):
			got, content, l.Remote.Cache = cached.entry, cached.content, "revalidated"
			got.Fetched = c.now()
		case hit && errors.As(err, &fetchErr):
			got, content, l.Remote.Cache = cached.entry, cached.content, "stale"
			l.Warnings = append(l.Warnings, Warning{
				Code: WarnStaleInclude, Path: l.Path,
				Message: fmt.Sprintf("%s: using a cached copy from %s, because fetching it failed: %v", l.Path,
					cached.entry.Fetched.Format("2006-01-02 15:04:05Z07:00"), err),
			})
		default:
			if cerr := ld.ctx.Err(); cerr != nil {
				return fail(contextError(cerr), "")
			}
			return fail(err, "")
		}
		if l.Remote.Cache == "" {
			if err := checkIdentity(fe.Identity); err != nil {
				return fail(&FetchError{BaseError: about(fmt.Errorf("%q: the resolver returned %w", raw, err))}, "")
			}
			content = fe.Content
			got = cacheEntry{
				Identity: fe.Identity, Sum: sha256Hex(content), MediaType: fe.MediaType, Fetched: c.now(), Pinned: rr.pinned,
				Validator: keptValidator(fe.Validator),
			}
			if got.Identity == "" {
				got.Identity = "sha256:" + got.Sum
			}
		}
	}
	l.Remote.Identity = got.Identity
	if int64(len(content)) > c.maxIncludeSize {
		return fail(&TooLargeError{BaseError: inside(fmt.Errorf("the content is %d bytes, and the limit is %d", len(content), c.maxIncludeSize))}, "")
	}
	if rr.sum != "" && sha256Hex(content) != rr.sum {
		return reject("doesn't match its integrity fragment: the content's SHA-256 is "+sha256Hex(content), "the content doesn't match its integrity fragment")
	}
	format, ok := ld.remoteFormat(rr, got.MediaType)
	if !ok {
		return fail(&UnsupportedFormatError{BaseError: inside(fmt.Errorf("no format rule, extension or media type (%q) has a registered decoder", got.MediaType))}, "")
	}
	l.Format = format
	if l.Remote.Cache == "" || l.Remote.Cache == "revalidated" {
		ld.cacheStore(rr.ref.Ref, got, content)
	}
	if ld.rec != nil {
		ld.rec.remotes = append(ld.rec.remotes, remoteUse{resolver: resolver, rr: rr, from: from, pol: pol, name: name})
	}
	return &entry{layer: l, data: content, included: true, remote: &rr}
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
