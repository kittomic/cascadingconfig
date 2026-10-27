package cascadingconfig

import (
	"fmt"
	"slices"
	"strings"
)

// addValues adds a value scope's tree, without the unset key, which a value scope can't use.
func (ld *loading) addValues(s *scopeDef, t Tree) {
	ld.addValuesLayer(Layer{Scope: s.name, Trust: s.trust, Status: LayerLoaded}, t)
}

// addValuesLayer adds a value layer l with tree t, without the unset key. A tree with no keys is LayerEmpty (DL-264).
func (ld *loading) addValuesLayer(l Layer, t Tree) {
	s := l.Scope
	if key := ld.c.merge.unsetKey; key != "" && dropKey(t, key) {
		l.warn(WarnReservedKey, "the scope %q can't remove keys, so its %s keys were ignored", s, key)
	}
	if len(t) == 0 {
		l.Status = LayerEmpty
	}
	ld.entries = append(ld.entries, &entry{layer: l, tree: t, value: true})
}

// dropKey removes key from every table in v, and reports whether there was one.
func dropKey(v any, key string) bool {
	found := false
	switch v := v.(type) {
	case map[string]any:
		if _, ok := v[key]; ok {
			delete(v, key)
			found = true
		}
		for _, e := range v {
			found = dropKey(e, key) || found
		}
	case []any:
		for _, e := range v {
			found = dropKey(e, key) || found
		}
	}
	return found
}

// envVar is one environment variable.
type envVar struct{ name, value string }

// matchingEnv returns the environment variables whose names start with prefix, sorted by name, leaving out those a
// binding reads (DL-277). On Windows, names compare without regard to case.
func (ld *loading) matchingEnv(prefix string) []envVar {
	h := ld.c.host
	windows := h.GOOS() == "windows"
	norm := func(name string) string {
		if windows {
			return strings.ToUpper(name)
		}
		return name
	}
	bound := map[string]bool{}
	for _, v := range ld.c.vars {
		bound[norm(v)] = true
	}
	var vars []envVar
	for _, kv := range h.Environ() {
		name, value, ok := strings.Cut(kv, "=")
		if !ok || name == "" || len(name) < len(prefix) || bound[norm(name)] {
			continue
		}
		if p := name[:len(prefix)]; p != prefix && (!windows || !strings.EqualFold(p, prefix)) {
			continue
		}
		vars = append(vars, envVar{name, value})
	}
	slices.SortFunc(vars, func(a, b envVar) int { return strings.Compare(a.name, b.name) })
	return vars
}

// addEnvVars adds an EnvVars scope's values.
func (ld *loading) addEnvVars(s *scopeDef, k envVarsKind) {
	vars := ld.matchingEnv(k.prefix)
	t := Tree{}
	type setBy struct {
		key  Path
		name string
	}
	var sets []setBy // the variable behind each key, without those a later variable replaced
	l := Layer{Scope: s.name, Trust: s.trust, Status: LayerLoaded}
	for _, v := range vars {
		var key Path
		var value any = v.value
		if k.mapper != nil {
			var ok bool
			key, value, ok = k.mapper(v.name, v.value)
			if !ok {
				continue
			}
			if len(key) == 0 {
				l.warn(WarnEnvVarSkipped, "%s: the mapping gave an empty key, so it was skipped", v.name)
				continue
			}
			if err := checkCanonical(value, key); err != nil {
				ld.addError(NewError(CodeInternal, "MapEnv", "", fmt.Errorf("the variable %s: the function returned %w", v.name, err)))
				return
			}
		} else {
			key = strings.Split(v.name[len(k.prefix):], "__")
			if slices.Contains(key, "") {
				l.warn(WarnEnvVarSkipped, "%s: the name has an empty segment, so it was skipped", v.name)
				continue
			}
			for i := range key {
				key[i] = strings.ToLower(key[i])
			}
		}
		if setPath(t, key, value) {
			l.warn(WarnEnvVarClash, "%s clashes with an earlier variable at %s; the later name wins", v.name, key)
		}
		sets = slices.DeleteFunc(sets, func(b setBy) bool { return hasPrefix(b.key, key) || hasPrefix(key, b.key) })
		sets = append(sets, setBy{slices.Clone(key), v.name})
	}
	if unset := ld.c.merge.unsetKey; unset != "" && dropKey(t, unset) {
		l.warn(WarnReservedKey, "the scope %q can't remove keys, so its %s keys were ignored", s.name, unset)
	}
	by := make(map[string]string, len(sets))
	for _, b := range sets {
		by[b.key.String()] = b.name
	}
	if len(t) == 0 {
		l.Status = LayerEmpty // no variable supplied a key (DL-264)
	}
	ld.entries = append(ld.entries, &entry{layer: l, tree: t, value: true, vars: by})
}

// setPath sets the value at key in t, replacing anything in the way, and reports whether it replaced something.
func setPath(t Tree, key Path, value any) bool {
	clash := false
	m := t
	for _, k := range key[:len(key)-1] {
		next, ok := m[k].(map[string]any)
		if !ok {
			if _, exists := m[k]; exists {
				clash = true
			}
			next = map[string]any{}
			m[k] = next
		}
		m = next
	}
	last := key[len(key)-1]
	if _, exists := m[last]; exists {
		clash = true
	}
	m[last] = value
	return clash
}
