package cascadingconfig

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/kittomic/cascadingconfig/internal/tree"
)

// BootstrapFiles makes a scope's kind also search for bootstrap files, in the same places as its config files. Only
// FixedDir and Walk support it. Bootstrap files are read before the main search and change how it runs; they never
// become part of the config.
func BootstrapFiles(patterns ...Pattern) ScopeOption {
	patterns = slices.Clone(patterns)
	return scopeOptionFunc(func(s *scopeSpec) {
		switch s.def.kind.(type) {
		case fixedDirKind, walkKind:
		default:
			s.c.fail("BootstrapFiles", "the scope %q can't have bootstrap files: only FixedDir and Walk scopes can", s.def.name)
			return
		}
		if len(patterns) == 0 {
			s.c.fail("BootstrapFiles", "no patterns")
			return
		}
		for _, p := range patterns {
			if err := checkPattern(p); err != nil {
				s.c.fail("BootstrapFiles", "%w", err)
				return
			}
		}
		s.def.bootstrap = patterns
	})
}

// BootstrapKey is a key a bootstrap file may set.
type BootstrapKey string

// The bootstrap keys.
const (
	BootstrapEnv                     BootstrapKey = "env"
	BootstrapFormats                 BootstrapKey = "formats"
	BootstrapCeilingPaths            BootstrapKey = "ceiling_paths"
	BootstrapIgnoredConfigPaths      BootstrapKey = "ignored_config_paths"
	BootstrapOverrideConfigFilenames BootstrapKey = "override_config_filenames"

	// BootstrapSearchPlaces is cosmiconfig's searchPlaces: file names, which may use {name}, added to every walk's
	// patterns at the highest precedence.
	BootstrapSearchPlaces BootstrapKey = "searchPlaces"
	// BootstrapMergeSearchPlaces is cosmiconfig's mergeSearchPlaces: true by default; false makes searchPlaces replace
	// the walk's patterns.
	BootstrapMergeSearchPlaces BootstrapKey = "mergeSearchPlaces"
)

var bootstrapKeys = []BootstrapKey{
	BootstrapEnv, BootstrapFormats, BootstrapCeilingPaths, BootstrapIgnoredConfigPaths, BootstrapOverrideConfigFilenames,
	BootstrapSearchPlaces, BootstrapMergeSearchPlaces,
}

// WithBootstrapKeys enables bootstrap keys. A bootstrap file's other keys are warned about and ignored.
func WithBootstrapKeys(keys ...BootstrapKey) Option {
	keys = slices.Clone(keys)
	return optionFunc(func(c *config) {
		for _, k := range keys {
			if !slices.Contains(bootstrapKeys, k) {
				c.fail("WithBootstrapKeys", "%q isn't a bootstrap key", k)
				continue
			}
			if c.bootstrapKeys == nil {
				c.bootstrapKeys = map[BootstrapKey]bool{}
			}
			c.bootstrapKeys[k] = true
		}
	})
}

// WithoutBootstrapKey turns a bootstrap key off, so bootstrap files can't change that setting.
func WithoutBootstrapKey(key BootstrapKey) Option {
	return optionFunc(func(c *config) {
		if !slices.Contains(bootstrapKeys, key) {
			c.fail("WithoutBootstrapKey", "%q isn't a bootstrap key", key)
			return
		}
		delete(c.bootstrapKeys, key)
	})
}

// WithBootstrapLimit restricts the bootstrap files of one scope to the keys listed. Other keys are dropped with a
// warning.
func WithBootstrapLimit(scope ScopeName, keys ...BootstrapKey) Option {
	keys = slices.Clone(keys)
	return optionFunc(func(c *config) {
		for _, k := range keys {
			if !slices.Contains(bootstrapKeys, k) {
				c.fail("WithBootstrapLimit", "%q isn't a bootstrap key", k)
				return
			}
		}
		if c.bootstrapLimits == nil {
			c.bootstrapLimits = map[ScopeName][]BootstrapKey{}
		}
		c.bootstrapLimits[scope] = keys
	})
}

// WithBootstrapTransform adds a function that runs on each bootstrap file's tree, a copy, before its keys are read. It
// can rewrite or drop keys. An error it returns is a *TransformError naming the file.
func WithBootstrapTransform(f func(ctx context.Context, l Layer, t Tree) (Tree, error)) Option {
	return optionFunc(func(c *config) {
		if f == nil {
			c.fail("WithBootstrapTransform", "a nil function")
			return
		}
		c.bootstrapTransforms = append(c.bootstrapTransforms, f)
	})
}

// finishBootstrap checks what needs every option to have run.
func (c *config) finishBootstrap() {
	for _, scope := range slices.Sorted(mapKeys(c.bootstrapLimits)) {
		if c.scope(scope) == nil {
			c.fail("WithBootstrapLimit", "the scope %q isn't defined", scope)
		}
	}
}

func mapKeys[K comparable, V any](m map[K]V) func(yield func(K) bool) {
	return func(yield func(K) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}

// bootSettings is what the bootstrap files set, combined.
type bootSettings struct {
	env      []string // the highest file's env; nil when no file set it
	override []string // the highest file's override_config_filenames
	formats  []bootFormats
	ceilings []string
	ignored  []ignoredPath

	searchPlaces      []string // the highest file's searchPlaces; nil when no file set them
	mergeSearchPlaces bool     // whether searchPlaces add to a walk's patterns, rather than replace them
}

// bootFormats is one bootstrap file's formats, which apply to scopes at most as trusted as the file.
type bootFormats struct {
	trust   TrustLevel
	formats []string
}

// ignoredPath is one ignored path, with the trust of where it came from. An entry from a bootstrap file never applies
// to a more trusted scope.
type ignoredPath struct {
	path  string
	trust TrustLevel
	from  *entry // the bootstrap file it came from, for warnings; nil for the option and the env var
	drop  bool   // already warned about
}

// readBootstrap searches every selected scope's bootstrap files, lowest precedence first, and combines their keys.
func (ld *loading) readBootstrap() error {
	c := ld.c
	var errs []error
	for _, s := range c.scopes {
		if !c.selected[s.name] || len(s.bootstrap) == 0 {
			continue
		}
		ld.order = ld.baseOrder
		saved := ld.entries
		ld.entries = nil
		switch k := s.kind.(type) {
		case fixedDirKind:
			if dir, ok := ld.resolveDir(k.dir, s); ok {
				if root, ok := ld.root(dir); ok {
					if configRoot, ok := ld.scopeRoot(s, dir); ok {
						ld.search(root, dir, configRoot, s.bootstrap, s, true)
					}
				}
			}
		case walkKind:
			if dirs, ok := ld.walkDirs(s, true); ok {
				ld.walking = true
				for _, dir := range dirs {
					if root, ok := ld.root(dir); ok {
						ld.search(root, dir, dir, s.bootstrap, s, true)
					}
				}
				ld.walking = false
			}
		}
		for _, e := range ld.entries {
			if e.err != nil {
				errs = append(errs, e.err)
				continue
			}
			ld.boot = append(ld.boot, e)
		}
		ld.entries = saved
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	for _, e := range ld.boot {
		if err := ld.applyBootstrap(e); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// applyBootstrap decodes one bootstrap file, runs the transforms on it, and records its keys.
func (ld *loading) applyBootstrap(e *entry) error {
	c := ld.c
	if e.layer.Status != LayerLoaded {
		return nil
	}
	doc, err := DecodeDocument(c.decoder(e.layer.Format), e.layer.Path, e.data)
	if err != nil {
		return err
	}
	e.layer.Warnings = append(e.layer.Warnings, doc.Warnings...)
	if doc.Empty {
		e.layer.Status = LayerEmpty
		return nil
	}
	t := doc.Tree
	if e.key != nil {
		sub, ok, err := subtree(t, e.key)
		switch {
		case err != nil:
			return &ParseError{BaseError: BaseError{Op: "bootstrap", Path: e.layer.Path, Err: err}, Format: e.layer.Format}
		case !ok:
			e.layer.Status = LayerEmpty
			return nil
		}
		t = sub
	}
	for _, f := range c.bootstrapTransforms {
		next, err := f(ld.ctx, e.layer, tree.CopyMap(t))
		if err != nil {
			return &TransformError{BaseError: BaseError{
				Op: "bootstrap", Path: e.layer.Path, Scope: string(e.layer.Scope), Err: err,
			}}
		}
		t = next
	}

	warn := func(code WarningCode, format string, args ...any) {
		e.layer.Warnings = append(e.layer.Warnings, Warning{Code: code, Path: e.layer.Path, Message: fmt.Sprintf(format, args...)})
	}
	limit, limited := c.bootstrapLimits[e.layer.Scope]
	dir := filepath.Dir(e.layer.Path)
	for _, k := range tree.SortedKeys(t) {
		key := BootstrapKey(k)
		if !c.bootstrapKeys[key] {
			warn(WarnBootstrapUnknownKey, "%s: %q isn't a bootstrap key here, so it was ignored", e.layer.Path, k)
			continue
		}
		if limited && !slices.Contains(limit, key) {
			warn(WarnBootstrapKeyDropped, "%s: the %s scope's bootstrap files may not set %q, so it was dropped",
				e.layer.Path, e.layer.Scope, k)
			continue
		}
		if key == BootstrapMergeSearchPlaces {
			merge, ok := t[k].(bool)
			if !ok {
				return ld.bootError(e, key, fmt.Errorf("the value must be a boolean, not %s", describeType(t[k])))
			}
			ld.settings.mergeSearchPlaces = merge
			continue
		}
		list, err := stringList(t[k])
		if err != nil {
			return ld.bootError(e, key, err)
		}
		switch key {
		case BootstrapEnv:
			for _, env := range list {
				if err := checkName(env); err != nil {
					return ld.bootError(e, key, fmt.Errorf("invalid environment name %q: %w", env, err))
				}
			}
			ld.settings.env = list
		case BootstrapFormats:
			if len(list) == 0 {
				return ld.bootError(e, key, errors.New("an empty list would turn every format off"))
			}
			formats, errs := c.parseFormats(list, true)
			if len(errs) > 0 {
				return ld.bootError(e, key, errs[0])
			}
			ld.settings.formats = append(ld.settings.formats, bootFormats{trust: e.layer.Trust, formats: formats})
		case BootstrapCeilingPaths, BootstrapIgnoredConfigPaths:
			for _, p := range list {
				abs, ok := ld.bootPath(p, dir)
				if !ok {
					continue
				}
				if key == BootstrapCeilingPaths {
					ld.settings.ceilings = append(ld.settings.ceilings, abs)
				} else {
					ld.settings.ignored = append(ld.settings.ignored, ignoredPath{path: abs, trust: e.layer.Trust, from: e})
				}
			}
		case BootstrapOverrideConfigFilenames:
			for _, n := range list {
				if err := checkPattern(Pattern{Template: n}); err != nil {
					return ld.bootError(e, key, err)
				}
			}
			ld.settings.override = list
		case BootstrapSearchPlaces:
			for _, n := range list {
				if err := checkPattern(Pattern{Template: n}); err != nil {
					return ld.bootError(e, key, err)
				}
			}
			ld.settings.searchPlaces = list
		}
	}
	return nil
}

func (ld *loading) bootError(e *entry, key BootstrapKey, err error) error {
	return &OptionError{BaseError: BaseError{
		Op: "bootstrap", Path: e.layer.Path, Scope: string(e.layer.Scope), Err: fmt.Errorf("%s: %w", key, err),
	}}
}

// bootPath resolves a path from a bootstrap file: ~ is expanded, and a relative path is relative to the file's
// directory.
func (ld *loading) bootPath(p, dir string) (string, bool) {
	if isHomePath(p) || filepath.IsAbs(p) {
		abs, ok := ld.hostPath(p, nil)
		return abs, ok
	}
	return filepath.Join(dir, p), true
}

// stringList reads a bootstrap value that must be a list of strings.
func stringList(v any) ([]string, error) {
	list, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("the value must be a list, not %s", describeType(v))
	}
	out := make([]string, len(list))
	for i, e := range list {
		s, ok := e.(string)
		if !ok {
			return nil, fmt.Errorf("entry %d is %s, not a string", i+1, describeType(e))
		}
		out[i] = s
	}
	return out, nil
}

// orderFor returns the decoders a scope searches with: the env var's order if set, else the formats of the
// highest-precedence bootstrap file at least as trusted as the scope, else the app's or the preset's order.
func (ld *loading) orderFor(s *scopeDef) []Decoder {
	if !ld.formatsFromEnv {
		for i := len(ld.settings.formats) - 1; i >= 0; i-- {
			if f := ld.settings.formats[i]; f.trust >= s.trust {
				return ld.decoders(f.formats)
			}
		}
	}
	return ld.baseOrder
}

func (ld *loading) decoders(formats []string) []Decoder {
	out := make([]Decoder, len(formats))
	for i, f := range formats {
		out[i] = ld.c.decoder(f)
	}
	return out
}
