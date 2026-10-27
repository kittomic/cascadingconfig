package cascadingconfig

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"path/filepath"
	"slices"
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
	return p.resolveLocal(from, root, raw, ancestors)
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

// resolving is one include as it's resolved, local or remote: the reference, the file that wrote it, its chain's root
// file, whose scope's policy judges it, and the layer as it fills in.
type resolving struct {
	p          *processor
	from, root *entry
	raw        string
	l          Layer
	pol        *policy
	name       string // the policy's name, for messages
}

func (p *processor) newResolving(from, root *entry, raw string) *resolving {
	scope := root.layer.Scope
	return &resolving{p: p, from: from, root: root, raw: raw, l: p.ld.includeLayer(from, root, raw),
		pol: p.ld.c.policyFor(scope), name: policyName(scope)}
}

// about is the base of an error about the reference; inside, of one about the included file itself (DL-185).
func (li *resolving) about(err error) BaseError { return includeAbout(li.from, err) }

func (li *resolving) inside(err error) BaseError {
	return BaseError{Op: "include", Path: li.l.Path, Scope: string(li.l.Scope), Err: err}
}

func (li *resolving) fail(err error, reason string) *entry { return failInclude(li.l, err, reason) }

// reject fails the include as its policy's decision.
func (li *resolving) reject(why, reason string) *entry {
	err := &PolicyError{BaseError: li.about(fmt.Errorf("%q %s", li.raw, why))}
	return li.fail(err, "rejected by the "+li.name+" policy: "+reason)
}

// badRef fails the include for a reference that doesn't parse or can't be joined.
func (li *resolving) badRef(err error) *entry {
	return li.fail(&ReferenceError{BaseError: li.about(fmt.Errorf("%q: %w", li.raw, err))}, "")
}

// resolveLocal reads the local file that raw, from's include reference, names (DL-182, DL-183): it authorises the
// reference, opens and checks the file, then reads it.
func (p *processor) resolveLocal(from, root *entry, raw string, ancestors []*entry) *entry {
	li := p.newResolving(from, root, raw)
	target, failed := li.authorise()
	if failed != nil {
		return failed
	}
	r, info, failed := li.open(target, ancestors)
	if failed != nil {
		return failed
	}
	return li.read(r, info, target)
}

// authorise turns the reference into an absolute path and checks it against the policy: the file scheme, the
// confinement roots and the allow-lists. It returns the path with symlinks resolved.
func (li *resolving) authorise() (string, *entry) {
	ld, c, raw := li.p.ld, li.p.ld.c, li.raw
	if scheme, remote := remoteScheme(raw); remote {
		return "", li.fail(&UnsupportedSchemeError{BaseError: li.about(fmt.Errorf("%q: no resolver is registered for the scheme %q", raw, scheme))}, "")
	}
	abs, err := ld.localPath(raw, li.from.layer.Path)
	if err != nil {
		return "", li.badRef(err)
	}
	li.l.Path = abs
	ld.watchFile(abs)
	if !li.pol.usable(c, "file") {
		return "", li.reject(fmt.Sprintf(`can't be included: the %s policy doesn't allow the scheme "file"`, li.name), `the scheme "file" isn't allowed`)
	}
	target, err := c.fsys.EvalSymlinks(abs)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", li.fail(&NotFoundError{BaseError: li.about(fmt.Errorf("%q: %s doesn't exist", raw, abs))}, "")
		}
		return "", li.fail(&IOError{BaseError: li.inside(err)}, "")
	}
	if li.pol.confine != nil && !ld.confined(li.pol, li.root, target) {
		return "", li.reject(fmt.Sprintf("resolves to %s, outside the confinement roots of the %s policy", target, li.name), "outside the confinement roots")
	}
	ref := IncludeRef{Raw: raw, Scheme: "file", Ref: filepath.ToSlash(target), Location: filepath.ToSlash(target), From: li.from.layer}
	if !allowed(li.pol.allow, ref, c.name) || !allowed(li.pol.allowFor["file"], ref, c.name) {
		return "", li.reject(fmt.Sprintf("(%s) isn't on the %s policy's allow-list", ref.Location, li.name), "not on its allow-list")
	}
	return target, nil
}

// open stats the file and checks it before it's read: not already in this chain of includes, of a known format, a
// regular file and within the size limit.
func (li *resolving) open(target string, ancestors []*entry) (Root, fs.FileInfo, *entry) {
	ld, c := li.p.ld, li.p.ld.c
	dir, base := filepath.Dir(target), filepath.Base(target)
	r, ok := ld.root(dir)
	if !ok {
		return nil, nil, li.fail(&IOError{BaseError: li.inside(fmt.Errorf("its directory %s can't be opened", dir))}, "")
	}
	info, err := r.Stat(base)
	if err != nil {
		return nil, nil, li.fail(&IOError{BaseError: li.inside(err)}, "")
	}
	for _, a := range ancestors {
		if a.info != nil && c.fsys.SameFile(a.info, info) {
			err := &CycleError{BaseError: li.about(fmt.Errorf("%q includes %s, which is already in this chain of includes", li.raw, li.l.Path))}
			return nil, nil, li.fail(err, "an include cycle")
		}
	}
	format, ok := c.fileFormat(filepath.ToSlash(li.l.Path))
	if !ok {
		return nil, nil, li.fail(&UnsupportedFormatError{BaseError: li.inside(fmt.Errorf("no format rule or registered decoder's extension matches %s", li.l.Path))}, "")
	}
	li.l.Format = format
	if !info.Mode().IsRegular() {
		return nil, nil, li.fail(&IOError{BaseError: li.inside(fmt.Errorf("it is %s, not a regular file", describeMode(info.Mode())))}, "")
	}
	if info.Size() > c.maxFileSize {
		return nil, nil, li.fail(tooLarge(li.l, info.Size(), c.maxFileSize), "")
	}
	return r, info, nil
}

// read applies the policy's permission check and reads the file. The included file takes the scope and trust of its
// most trusted route, if discovery also found it (DL-184).
func (li *resolving) read(r Root, info fs.FileInfo, target string) *entry {
	ld, c := li.p.ld, li.p.ld.c
	e := &entry{layer: li.l, info: info, file: true, included: true, real: target}
	if di, err := r.Stat("."); err == nil {
		e.dirInfo = di
	}
	if v, why := ld.checkPerms(li.pol, e); v != PermissionAllow {
		return li.fail(&PolicyError{BaseError: li.inside(fmt.Errorf("the %s policy's file permission check failed: %s", li.name, why))},
			"rejected by the "+li.name+" policy: "+why)
	}
	base := filepath.Base(target)
	data, err := readLimited(r, base, c.maxFileSize)
	if err != nil {
		return li.fail(&IOError{BaseError: li.inside(err)}, "")
	}
	if int64(len(data)) > c.maxFileSize {
		return li.fail(tooLarge(li.l, int64(len(data)), c.maxFileSize), "")
	}
	e.data = data
	ld.watchLoaded(li.l.Path, target)
	for _, f := range li.p.found {
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
