package cascadingconfig

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
)

// process turns the found entries into the layers to merge. It walks them highest precedence first, so the first time
// a file is seen is its highest position (DL-179): each file gets its policy's permission and trust checks, is decoded
// and transformed, and has its include key read; its includes are walked directly after it, last entry first, so they
// rank directly below it. A file seen again is LayerDuplicate. ld.entries is replaced by every entry considered,
// included files among them, lowest precedence first.
//
// With first-match resolution, the highest-precedence non-empty file is chosen, with its includes; the files below it
// are ignored without being decoded, and the first problem found above it fails the load on its own.
func (ld *loading) process() ([]mergeLayer, error) {
	p := &processor{ld: ld, first: ld.c.resolution == FirstMatch, found: ld.entries}
	for i := len(ld.entries) - 1; i >= 0; i-- {
		e := ld.entries[i]
		if p.firstErr != nil {
			p.out = append(p.out, e)
			continue
		}
		if p.visit(e, nil) && p.first && p.chosen == nil {
			p.chosen = e
		}
	}
	slices.Reverse(p.out)
	slices.Reverse(p.layers)
	ld.entries = p.out
	if p.firstErr != nil {
		return nil, p.firstErr
	}
	var errs []error
	for _, e := range ld.entries {
		if e.err != nil {
			errs = append(errs, e.err)
		}
	}
	return p.layers, errors.Join(errs...)
}

// processor is the state of one process walk.
type processor struct {
	ld     *loading
	first  bool         // first-match resolution
	found  []*entry     // the entries discovery found, for each included file's most trusted route
	kept   []*entry     // the files walked so far, by identity
	out    []*entry     // every entry considered, highest precedence first
	layers []mergeLayer // highest precedence first
	chosen *entry       // the first-match result

	refs       int                  // include references followed so far, for WithMaxIncludes
	remoteSeen map[string]ScopeName // the remote files walked so far, by normalised reference, with their scopes

	firstErr error // with first-match resolution, the problem that fails the load
}

// visit processes one entry, and its includes after it. chain holds its includers, the root file first. It reports
// whether the entry is a file with config to merge.
func (p *processor) visit(e *entry, chain []*entry) bool {
	ld := p.ld
	p.out = append(p.out, e)
	top := len(chain) == 0
	if e.err != nil {
		if p.chosen != nil && top {
			e.err, e.dropped = nil, true
			return false
		}
		p.noteErr(e.err)
		return false
	}
	if e.layer.Status != LayerLoaded {
		return false
	}
	if e.value {
		if err := ld.transformLayer(e); err != nil {
			p.fail(e, err)
			return false
		}
		p.layers = append(p.layers, mergeLayer{layer: e.layer, tree: e.tree, vars: e.vars})
		return false
	}
	if p.chosen != nil && top {
		e.layer.Status = LayerIgnored
		e.layer.Reason = "first-match resolution chose " + p.chosen.layer.Path
		return false
	}
	if e.info != nil {
		for _, k := range p.kept {
			if ld.c.fsys.SameFile(k.info, e.info) {
				e.layer.Status = LayerDuplicate
				e.layer.Reason = fmt.Sprintf("loaded at a higher position through the %s scope", k.layer.Scope)
				return false
			}
		}
		p.kept = append(p.kept, e)
	}
	if !e.included && !ld.checkFile(ld.c.policyFor(e.layer.Scope), e) {
		if e.err != nil {
			p.noteErr(e.err)
		}
		return false
	}
	ok, err := ld.decodeEntry(e)
	if err != nil {
		e.err = err
		p.noteErr(err)
		return false
	}
	if !ok {
		return false
	}
	if err := ld.transformLayer(e); err != nil {
		p.fail(e, err)
		return false
	}
	refs, err := ld.includeRefs(e, !top)
	if err != nil {
		p.fail(e, err)
		return false
	}
	p.layers = append(p.layers, mergeLayer{layer: e.layer, tree: e.tree})
	ancestors := append(slices.Clone(chain), e)
	for i := len(refs) - 1; i >= 0; i-- {
		p.visit(p.resolve(e, ancestors, refs[i]), ancestors)
	}
	return true
}

// fail records a problem with an entry that was being loaded.
func (p *processor) fail(e *entry, err error) {
	e.err = err
	e.layer.Status, e.layer.Reason = LayerFailed, failReason(err)
	p.noteErr(err)
}

func (p *processor) noteErr(err error) {
	if p.first && p.firstErr == nil {
		p.firstErr = err
	}
}

// checkFile runs a file's permission check and trust requirement (DL-191, DL-192), and reports whether it may load. A
// searched file that fails is skipped with a warning; a file named directly, or a permission check's error, fails the
// load, with e.err set.
func (ld *loading) checkFile(pol *policy, e *entry) bool {
	if !e.file {
		return true
	}
	name := policyName(e.layer.Scope)
	reject := func(err error, reason string) bool {
		e.err = &PolicyError{BaseError: BaseError{Op: "load", Path: e.layer.Path, Scope: string(e.layer.Scope), Err: err}}
		e.layer.Status, e.layer.Reason = LayerFailed, "rejected by the "+name+" policy: "+reason
		return false
	}
	if v, why := ld.checkPerms(pol, e); v != PermissionAllow {
		if v == PermissionError || e.explicit {
			return reject(fmt.Errorf("the %s policy's file permission check failed: %s", name, why), why)
		}
		e.layer.Status, e.layer.Reason = LayerIgnored, why
		e.layer.Warnings = append(e.layer.Warnings, Warning{
			Code: WarnFilePermissions, Path: e.layer.Path, Message: e.layer.Path + " ignored: " + why,
		})
		return false
	}
	if pol.trust == nil {
		return true
	}
	sum := sha256.Sum256(e.data)
	ok, err := pol.trust.Trusted(ld.ctx, e.real, hex.EncodeToString(sum[:]))
	switch {
	case err != nil:
		e.err = NewError(CodeInternal, "trust store", e.layer.Path, err)
		e.layer.Status, e.layer.Reason = LayerFailed, "the trust store failed"
		return false
	case ok:
		return true
	case e.explicit:
		return reject(fmt.Errorf("the %s policy requires approval, and its trust store hasn't approved %s", name, e.real),
			"not approved by its trust store")
	}
	e.layer.Status, e.layer.Reason = LayerUntrusted, "its trust store hasn't approved it"
	e.layer.Warnings = append(e.layer.Warnings, Warning{
		Code: WarnUntrusted, Path: e.layer.Path, Message: e.layer.Path + " skipped: its trust store hasn't approved it",
	})
	return false
}

// checkPerms runs the policy's permission check on a file, then on its containing directory.
func (ld *loading) checkPerms(pol *policy, e *entry) (Permission, string) {
	if pol.perms == nil || e.info == nil {
		return PermissionAllow, ""
	}
	h := ld.c.host
	fc := FileCheck{Path: e.real, Info: e.info, Scope: e.layer.Scope, UID: h.UID(), GOOS: h.GOOS()}
	if v, why := pol.perms(fc); v != PermissionAllow {
		return v, why
	}
	if e.dirInfo == nil {
		return PermissionAllow, ""
	}
	fc.Path, fc.Info, fc.Dir = filepath.Dir(e.real), e.dirInfo, true
	if v, why := pol.perms(fc); v != PermissionAllow {
		return v, fmt.Sprintf("its directory %s is %s", fc.Path, why)
	}
	return PermissionAllow, ""
}

// includeRefs reads and removes the include key at the top of e's tree (DL-184, DL-187). nested says whether e was
// included itself.
func (ld *loading) includeRefs(e *entry, nested bool) ([]string, error) {
	key := ld.c.includeKey
	if key == "" || (!e.file && e.remote == nil) || e.tree == nil {
		return nil, nil
	}
	v, ok := e.tree[key]
	if !ok {
		return nil, nil
	}
	delete(e.tree, key)
	bad := func(format string, args ...any) error {
		return &ParseError{
			BaseError: BaseError{Op: "decode", Path: e.layer.Path, Scope: string(e.layer.Scope), Err: fmt.Errorf(format, args...)},
			Format:    e.layer.Format,
		}
	}
	if nested && ld.c.nesting == IncludeFlat {
		return nil, bad("the include key %q isn't allowed in an included file, because includes don't nest here", key)
	}
	switch v := v.(type) {
	case string:
		if v == "" {
			return nil, bad("the include key %q holds an empty reference", key)
		}
		return []string{v}, nil
	case []any:
		refs := make([]string, 0, len(v))
		for _, el := range v {
			s, ok := el.(string)
			switch {
			case !ok:
				return nil, bad("the include key %q holds %s, not a reference", key, describeType(el))
			case s == "":
				return nil, bad("the include key %q holds an empty reference", key)
			}
			refs = append(refs, s)
		}
		return refs, nil
	default:
		return nil, bad("the include key %q must hold a reference or a list of references, not %s", key, describeType(v))
	}
}
