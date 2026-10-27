package cascadingconfig

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// expansion is one pattern for one environment, or for none.
type expansion struct {
	p   Pattern
	env string
}

// expansions orders a scope's patterns for one directory: shared, then env once per environment, then local, then
// env-local, keeping the listed order within each group.
func (ld *loading) expansions(patterns []Pattern) []expansion {
	var out []expansion
	group := func(v Variant, env string) {
		for _, p := range patterns {
			if p.Variant == v {
				out = append(out, expansion{p, env})
			}
		}
	}
	group(VariantShared, "")
	for _, env := range ld.envs {
		group(VariantEnv, env)
	}
	group(VariantLocal, "")
	for _, env := range ld.envs {
		group(VariantEnvLocal, env)
	}
	return out
}

// search finds each pattern's files in root, in pattern order. dir is root's path, or empty for an fs.FS, whose layers
// are named by their slash path. configRoot is the root of RootSearchDir patterns. follow says whether symlinks can be
// followed through the loader's FileSystem.
func (ld *loading) search(root Root, dir, configRoot string, patterns []Pattern, s *scopeDef, follow bool) {
	ld.watchSearch(dir, patterns)
	skip := ld.walking && ld.isHomeOrRoot(dir)
	for _, x := range ld.expansions(patterns) {
		if skip && x.p.SkipHomeAndRoot {
			continue
		}
		segs := strings.Split(x.p.Template, "/")
		subs := []string{"."}
		for _, seg := range segs[:len(segs)-1] {
			subs = ld.matchDirs(root, dir, subs, expand(seg, ld.c.name, x.env, ""))
		}
		last := segs[len(segs)-1]
		found := false
		for _, sub := range subs {
			entries := ld.readDir(root, dir, sub)
			if entries == nil {
				continue
			}
			candidates := ld.candidates(last, x.env, entries, dir, sub)
			found = found || len(candidates) > 0
			for _, l := range candidates {
				l.Scope, l.Trust = s.name, s.trust
				l.Variant, l.Env, l.Pattern = x.p.Variant, x.env, x.p.Template
				l.ConfigRoot = configRoot
				if x.p.Root == RootFileDir {
					l.ConfigRoot = filepath.Dir(l.Path)
				}
				if ld.dropUnder != "" && within(l.Path, ld.dropUnder) {
					continue
				}
				if l.Status != LayerLoaded {
					ld.entries = append(ld.entries, &entry{layer: l})
					continue
				}
				name := path.Base(filepath.ToSlash(l.Path))
				var key Path
				if x.p.Key != "" {
					key, _ = parsePath(expand(x.p.Key, ld.c.name, "", ""))
				}
				ld.read(root, path.Join(sub, name), typeOf(entries, name), l, follow, key)
			}
		}
		if !found && ld.c.probes && !ld.booting {
			ld.probes = append(ld.probes, Probe{Scope: s.name, Dir: dir, Pattern: strings.ReplaceAll(x.p.Template, "{env}", x.env)})
		}
	}
}

func typeOf(entries []fs.DirEntry, name string) fs.FileMode {
	for _, e := range entries {
		if e.Name() == name {
			return e.Type()
		}
	}
	return 0
}

// readDir lists sub, a slash path in root, once per load. A missing directory gives nil; any other failure is a
// warning.
func (ld *loading) readDir(root Root, dir, sub string) []fs.DirEntry {
	key := dir + "\x00" + sub
	if entries, ok := ld.dirs[key]; ok {
		return entries
	}
	entries, err := root.ReadDir(sub)
	if err != nil {
		where := ld.join(dir, sub)
		switch {
		case errors.Is(err, fs.ErrNotExist):
		case underNonDirIn(root, sub):
			ld.notADir(where)
		default:
			ld.warnings = append(ld.warnings, Warning{
				Code: WarnUnreadable, Path: where, Message: fmt.Sprintf("%s can't be searched: %v", where, err),
			})
		}
		entries = nil
	}
	if entries == nil {
		entries = []fs.DirEntry{}
	}
	ld.dirs[key] = entries
	if len(entries) == 0 {
		return nil
	}
	return entries
}

// matchDirs extends each of subs by one directory segment, which may hold a * glob.
func (ld *loading) matchDirs(root Root, dir string, subs []string, seg string) []string {
	if !strings.Contains(seg, "*") {
		out := make([]string, len(subs))
		for i, sub := range subs {
			out[i] = path.Join(sub, seg)
		}
		return out
	}
	re := globPattern(seg, false)
	var out []string
	for _, sub := range subs {
		for _, e := range ld.readDir(root, dir, sub) {
			if e.IsDir() && !strings.HasPrefix(e.Name(), ".") && re.MatchString(e.Name()) {
				out = append(out, path.Join(sub, e.Name()))
			}
		}
	}
	return out
}

// globPattern turns a path segment with * into a regexp. In a file name, * matches a non-empty name with no dots; in a
// directory, any non-empty name.
func globPattern(seg string, file bool) *regexp.Regexp {
	star := ".+"
	if file {
		star = "[^.]+"
	}
	parts := strings.Split(seg, "*")
	for i, p := range parts {
		parts[i] = regexp.QuoteMeta(p)
	}
	return regexp.MustCompile("^" + strings.Join(parts, "("+star+")") + "$")
}

func (ld *loading) join(dir, slashPath string) string {
	if dir == "" {
		return slashPath
	}
	return filepath.Join(dir, filepath.FromSlash(slashPath))
}

// candidates returns the files in one directory that match a file-name template, lowest precedence first. With
// {ext}, the files with the same stem form a slot ordered by orderSlot; slots apply in alphabetical order.
func (ld *loading) candidates(last, env string, entries []fs.DirEntry, dir, sub string) []Layer {
	name := ld.c.name
	type match struct {
		file, format string
	}
	slots := map[string][]match{}
	add := func(e, format string) {
		if env != "" && len(e) > 255 {
			ld.envNameError(env, fmt.Errorf("the file name %q is longer than 255 bytes", e))
			return
		}
		if !strings.Contains(e, "*") {
			if hasEntry(entries, e) {
				if f, ok := ld.searchedFormat(dir, sub, e, format); ok {
					slots[""] = append(slots[""], match{e, f})
				}
			}
			return
		}
		re := globPattern(e, true)
		for _, en := range entries {
			if m := re.FindStringSubmatch(en.Name()); m != nil {
				if f, ok := ld.searchedFormat(dir, sub, en.Name(), format); ok {
					stem := strings.Join(m[1:], "\x00")
					slots[stem] = append(slots[stem], match{en.Name(), f})
				}
			}
		}
	}
	if !slices.Contains(placeholders(last), "{ext}") {
		e := expand(last, name, env, "")
		if strings.Contains(e, "*") {
			re := globPattern(e, true)
			for _, en := range entries {
				if re.MatchString(en.Name()) {
					if format, ok := ld.searchedFormat(dir, sub, en.Name(), ""); ok {
						slots[en.Name()] = append(slots[en.Name()], match{en.Name(), format})
					}
				}
			}
		} else {
			add(e, "")
		}
	} else {
		for _, d := range ld.order {
			for _, ext := range d.Extensions() {
				add(expand(last, name, env, ext), d.Format())
			}
		}
	}

	var out []Layer
	for _, stem := range slices.Sorted(maps.Keys(slots)) {
		var slot []Layer
		for _, m := range slots[stem] {
			slot = append(slot, Layer{Path: ld.join(dir, path.Join(sub, m.file)), Format: m.format})
		}
		ordered, err := orderSlot(slot, ld.order, ld.c.conflict)
		if err != nil {
			ld.addError(err)
			continue
		}
		out = append(out, ordered...)
	}
	return out
}

func hasEntry(entries []fs.DirEntry, name string) bool {
	return slices.ContainsFunc(entries, func(e fs.DirEntry) bool { return e.Name() == name })
}

// searchedFormat returns the format of a candidate named file in sub of dir, from the file-format rules or else
// fallback (the decoder whose extension {ext} stood for) or the file's extension. It must be one the scope searches.
func (ld *loading) searchedFormat(dir, sub, file, fallback string) (string, bool) {
	p := filepath.ToSlash(ld.join(dir, path.Join(sub, file)))
	format, ok := ld.c.fileFormat(p)
	if fallback != "" && !ld.c.ruleMatches(p) {
		format, ok = fallback, true
	}
	if !ok {
		return "", false
	}
	for _, d := range ld.order {
		if d.Format() == format {
			return format, true
		}
	}
	return "", false
}

// overrideFilenames applies {NAME}_OVERRIDE_CONFIG_FILENAMES to a walk's patterns: the listed names replace its
// shared patterns, and its local patterns are removed.
func (ld *loading) overrideFilenames(patterns []Pattern) ([]Pattern, bool) {
	names := strings.Split(ld.binding(BindOverrideConfigFilenames), ",")
	if ld.binding(BindOverrideConfigFilenames) == "" {
		if ld.settings.override == nil {
			return patterns, true
		}
		names = ld.settings.override
	}
	var out []Pattern
	for _, n := range names {
		if n = strings.TrimSpace(n); n == "" {
			continue
		}
		p := Pattern{Template: n}
		if err := checkPattern(p); err != nil {
			ld.addError(&OptionError{BaseError: BaseError{Op: ld.c.vars[BindOverrideConfigFilenames], Err: err}})
			return nil, false
		}
		out = append(out, p)
	}
	for _, p := range patterns {
		if p.Variant == VariantEnv || p.Variant == VariantEnvLocal {
			out = append(out, p)
		}
	}
	return out, true
}

// searchPlaces applies a bootstrap file's searchPlaces to a walk's patterns: added at the highest precedence, or,
// with mergeSearchPlaces false, replacing them.
func (ld *loading) searchPlaces(patterns []Pattern) []Pattern {
	if ld.settings.searchPlaces == nil {
		return patterns
	}
	var places []Pattern
	for _, n := range ld.settings.searchPlaces {
		places = append(places, Pattern{Template: n})
	}
	if !ld.settings.mergeSearchPlaces {
		return places
	}
	return append(slices.Clone(patterns), places...)
}
