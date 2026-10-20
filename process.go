package cascadingconfig

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
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

// resolveInclude reads the file that from's reference raw names, as an entry to walk: a loaded one, or a failed one
// holding the error. root is the first file of the chain, whose scope's policy judges the reference and whose metadata
// the included layer takes (DL-184); ancestors are the chain, for cycles.
func (ld *loading) resolveInclude(from, root *entry, raw string, ancestors, found []*entry) *entry {
	c := ld.c
	scope := root.layer.Scope
	pol := c.policyFor(scope)
	name := policyName(scope)
	l := Layer{
		Path: raw, Scope: scope, Variant: root.layer.Variant, Env: root.layer.Env, Trust: root.layer.Trust,
		ConfigRoot: root.layer.ConfigRoot, IncludedBy: from.layer.Path, Status: LayerLoaded,
	}
	about := func(err error) BaseError {
		return BaseError{Op: "include", Path: from.layer.Path, Scope: string(from.layer.Scope), Err: err}
	}
	inside := func(err error) BaseError {
		return BaseError{Op: "include", Path: l.Path, Scope: string(scope), Err: err}
	}
	fail := func(err error, reason string) *entry {
		if reason == "" {
			reason = failReason(err)
		}
		l.Status, l.Reason = LayerFailed, reason
		return &entry{layer: l, err: err, included: true}
	}
	reject := func(why, reason string) *entry {
		return fail(&PolicyError{BaseError: about(fmt.Errorf("%q %s", raw, why))}, "rejected by the "+name+" policy: "+reason)
	}

	if scheme, remote := remoteScheme(raw); remote {
		return fail(&UnsupportedSchemeError{BaseError: about(fmt.Errorf("%q: no resolver is registered for the scheme %q", raw, scheme))}, "")
	}
	abs, err := ld.localPath(raw, from.layer.Path)
	if err != nil {
		return fail(&ReferenceError{BaseError: about(fmt.Errorf("%q: %w", raw, err))}, "")
	}
	l.Path = abs
	if !pol.usable(c, "file") {
		return reject(fmt.Sprintf(`can't be included: the %s policy doesn't allow the scheme "file"`, name), `the scheme "file" isn't allowed`)
	}
	target, err := c.fsys.EvalSymlinks(abs)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fail(&NotFoundError{BaseError: about(fmt.Errorf("%q: %s doesn't exist", raw, abs))}, "")
		}
		return fail(&IOError{BaseError: inside(err)}, "")
	}
	if pol.confine != nil && !ld.confined(pol, root, target) {
		return reject(fmt.Sprintf("resolves to %s, outside the confinement roots of the %s policy", target, name), "outside the confinement roots")
	}
	ref := IncludeRef{Raw: raw, Scheme: "file", Ref: filepath.ToSlash(target), Location: filepath.ToSlash(target), From: from.layer}
	if !allowed(pol.allow, ref, c.name) || !allowed(pol.allowFor["file"], ref, c.name) {
		return reject(fmt.Sprintf("(%s) isn't on the %s policy's allow-list", ref.Location, name), "not on its allow-list")
	}

	dir, base := filepath.Dir(target), filepath.Base(target)
	r, ok := ld.root(dir)
	if !ok {
		return fail(&IOError{BaseError: inside(fmt.Errorf("its directory %s can't be opened", dir))}, "")
	}
	info, err := r.Stat(base)
	if err != nil {
		return fail(&IOError{BaseError: inside(err)}, "")
	}
	for _, a := range ancestors {
		if a.info != nil && c.fsys.SameFile(a.info, info) {
			return fail(&CycleError{BaseError: about(fmt.Errorf("%q includes %s, which is already in this chain of includes", raw, abs))},
				"an include cycle")
		}
	}
	format, ok := c.fileFormat(filepath.ToSlash(abs))
	if !ok {
		return fail(&UnsupportedFormatError{BaseError: inside(fmt.Errorf("no format rule or registered decoder's extension matches %s", abs))}, "")
	}
	l.Format = format
	if !info.Mode().IsRegular() {
		return fail(&IOError{BaseError: inside(fmt.Errorf("it is %s, not a regular file", describeMode(info.Mode())))}, "")
	}
	if info.Size() > c.maxFileSize {
		return fail(tooLarge(l, info.Size(), c.maxFileSize), "")
	}
	e := &entry{layer: l, info: info, file: true, included: true, real: target}
	if di, err := r.Stat("."); err == nil {
		e.dirInfo = di
	}
	if v, why := ld.checkPerms(pol, e); v != PermissionAllow {
		return fail(&PolicyError{BaseError: inside(fmt.Errorf("the %s policy's file permission check failed: %s", name, why))},
			"rejected by the "+name+" policy: "+why)
	}
	data, err := readLimited(r, base, c.maxFileSize)
	if err != nil {
		return fail(&IOError{BaseError: inside(err)}, "")
	}
	if int64(len(data)) > c.maxFileSize {
		return fail(tooLarge(l, int64(len(data)), c.maxFileSize), "")
	}
	e.data = data
	for _, f := range found {
		if f.info != nil && c.fsys.SameFile(f.info, info) && f.layer.Trust > e.layer.Trust {
			e.layer.Scope, e.layer.Trust = f.layer.Scope, f.layer.Trust
		}
	}
	return e
}

// remoteScheme returns a reference's scheme when it's remote: name::… or scheme://… with a scheme of two or more
// letters, other than file (DL-183).
func remoteScheme(raw string) (string, bool) {
	if i := strings.Index(raw, "::"); i > 0 && isScheme(raw[:i]) {
		return strings.ToLower(raw[:i]), true
	}
	if i := strings.Index(raw, "://"); i >= 2 && isScheme(raw[:i]) {
		if s := strings.ToLower(raw[:i]); s != "file" {
			return s, true
		}
	}
	return "", false
}

func isScheme(s string) bool {
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		case i > 0 && (r >= '0' && r <= '9' || r == '+' || r == '-' || r == '.'):
		default:
			return false
		}
	}
	return s != ""
}

// localPath returns the absolute path a local reference names: a file:// URL with an empty host or localhost, a path
// starting with ~, an absolute path, or a path relative to the including file's directory.
func (ld *loading) localPath(raw, from string) (string, error) {
	if len(raw) >= len("file://") && strings.EqualFold(raw[:len("file://")], "file://") {
		u, err := url.Parse(raw)
		switch {
		case err != nil:
			return "", err
		case u.Host != "" && u.Host != "localhost":
			return "", fmt.Errorf("a file URL may only name the host localhost, not %q", u.Host)
		case !strings.HasPrefix(u.Path, "/"):
			return "", errors.New("the file URL has no absolute path")
		}
		return filepath.Clean(filepath.FromSlash(u.Path)), nil
	}
	if isHomePath(raw) {
		home, err := ld.c.host.Home()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, raw[1:]), nil
	}
	if filepath.IsAbs(raw) {
		return filepath.Clean(raw), nil
	}
	return filepath.Join(filepath.Dir(from), filepath.FromSlash(raw)), nil
}

// confined reports whether target, a resolved path, is one of the policy's roots or below one (DL-180).
func (ld *loading) confined(pol *policy, root *entry, target string) bool {
	for _, r := range pol.confine {
		dir, ok := ld.confineRoot(r, root)
		if !ok {
			continue
		}
		if resolved, err := ld.c.fsys.EvalSymlinks(dir); err == nil && within(target, resolved) {
			return true
		}
	}
	return false
}

// confineRoot fills in a Confine root's placeholder. A root that can't be worked out, such as {user} without platform
// directories or a home, matches nothing.
func (ld *loading) confineRoot(r string, root *entry) (string, bool) {
	var dir string
	switch {
	case r == "{config_root}" || strings.HasPrefix(r, "{config_root}/"):
		if root.layer.ConfigRoot == "" {
			return "", false
		}
		dir = filepath.Join(root.layer.ConfigRoot, filepath.FromSlash(strings.TrimPrefix(r, "{config_root}")))
	case strings.HasPrefix(r, "{user}"), strings.HasPrefix(r, "{system}"):
		if ld.c.dirs == nil {
			return "", false
		}
		fallthrough
	case strings.HasPrefix(r, "{cwd}"), isHomePath(r):
		var ok bool
		if dir, ok = ld.resolveDir(r, nil); !ok {
			return "", false
		}
	default:
		dir = filepath.Clean(r)
	}
	return dir, filepath.IsAbs(dir)
}

// allowed reports whether ref matches one of the list's matchers, or the list is empty.
func allowed(list []Matcher, ref IncludeRef, name string) bool {
	if len(list) == 0 {
		return true
	}
	return slices.ContainsFunc(list, func(m Matcher) bool { return matchRef(m, ref, name) })
}
