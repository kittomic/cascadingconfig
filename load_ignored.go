package cascadingconfig

import (
	"fmt"
	"slices"
)

// ignoredPaths resolves WithIgnoredPaths and {NAME}_IGNORED_CONFIG_PATHS.
func (ld *loading) ignoredPaths() {
	paths := slices.Clone(ld.c.ignored)
	if v := ld.binding(BindIgnoredConfigPaths); v != "" {
		paths = append(paths, ld.pathList(v)...)
	}
	for _, p := range paths {
		abs, ok := ld.hostPath(p, nil)
		if !ok {
			continue
		}
		ld.ignored = append(ld.ignored, ignoredPath{path: ld.realPath(abs), trust: TrustSystem})
	}
	for _, p := range ld.settings.ignored {
		p.path = ld.realPath(p.path)
		ld.ignored = append(ld.ignored, p)
	}
}

// isIgnored reports whether a file in a scope with trust, whose resolved path is resolved, is under an ignored path.
// An entry from a less trusted bootstrap file is dropped for it, with a warning on that file.
func (ld *loading) isIgnored(resolved string, trust TrustLevel) (string, bool) {
	for i := range ld.ignored {
		p := &ld.ignored[i]
		if !within(resolved, p.path) {
			continue
		}
		if p.trust >= trust {
			return p.path, true
		}
		if !p.drop && p.from != nil {
			p.drop = true
			p.from.layer.warn(WarnBootstrapKeyDropped, "%s: the ignored path %s points into a more trusted scope, so it was dropped for %s",
				p.from.layer.Path, p.path, resolved)
		}
	}
	return "", false
}

// ignore records l as ignored, without a warning, if resolved is under an ignored path.
func (ld *loading) ignore(l Layer, resolved string) bool {
	p, ok := ld.isIgnored(resolved, l.Trust)
	if !ok {
		return false
	}
	l.Status = LayerIgnored
	l.Reason = fmt.Sprintf("it is under the ignored path %s", p)
	ld.entries = append(ld.entries, &entry{layer: l})
	return true
}
