package cascadingconfig

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Require is the level of a RequireConfig requirement: RequireAny, or a combination of the flags below.
type Require uint

// RequireAny counts any loaded layer, of any kind. It's the most forgiving level.
const RequireAny Require = 0

// Flags that narrow what counts. Combine them with |.
const (
	RequireFile     Require = 1 << iota // only layers read from files: discovery, explicit files and includes
	RequireNonEmpty                     // empty layers don't count
)

// RequireConfig makes a load fail with a *NotFoundError unless config is found at level. With no scopes, one
// qualifying layer anywhere is enough; with scopes, each of them must have one. Each RequireConfig adds a requirement,
// and all of them must hold.
//
// A value layer with no keys never counts. Explicit files count for the scope they replace, or for every scope under
// WithConfigFilesOnly. An unknown flag, or a scope that isn't defined or selected, is an *OptionError.
func RequireConfig(level Require, scopes ...ScopeName) Option {
	scopes = slices.Clone(scopes)
	return optionFunc(func(c *config) {
		if extra := level &^ (RequireFile | RequireNonEmpty); extra != 0 {
			c.fail("RequireConfig", "unknown flags %#x", uint(extra))
			return
		}
		c.requires = append(c.requires, requirement{level: level, scopes: scopes})
	})
}

// requirement is one RequireConfig.
type requirement struct {
	level  Require
	scopes []ScopeName
}

// finishRequirements checks that every required scope is defined and selected.
func (c *config) finishRequirements() {
	for _, r := range c.requires {
		for _, s := range r.scopes {
			switch {
			case c.scope(s) == nil:
				c.fail("RequireConfig", "the scope %q isn't defined", s)
			case !c.selected[s]:
				c.fail("RequireConfig", "the scope %q isn't selected, so it can never have config", s)
			}
		}
	}
}

// describe says what the level counts, for messages.
func (l Require) describe() string {
	switch {
	case l&RequireFile != 0 && l&RequireNonEmpty != 0:
		return "a non-empty config file"
	case l&RequireFile != 0:
		return "a config file"
	case l&RequireNonEmpty != 0:
		return "non-empty config"
	default:
		return "config"
	}
}

// checkRequirements returns a *NotFoundError for each requirement the load's layers don't meet.
func (ld *loading) checkRequirements() error {
	var errs []error
	for _, r := range ld.c.requires {
		if len(r.scopes) == 0 {
			if !slices.ContainsFunc(ld.entries, func(e *entry) bool { return ld.qualifies(e, r.level, "") }) {
				errs = append(errs, &NotFoundError{BaseError: BaseError{
					Op: "RequireConfig", Err: fmt.Errorf("no %s was found", r.level.describe()),
				}})
			}
			continue
		}
		var unmet []string
		for _, s := range r.scopes {
			if !slices.ContainsFunc(ld.entries, func(e *entry) bool { return ld.qualifies(e, r.level, s) }) {
				unmet = append(unmet, fmt.Sprintf("%q", s))
			}
		}
		if len(unmet) > 0 {
			scope := ""
			if len(unmet) == 1 {
				scope = strings.Trim(unmet[0], `"`)
			}
			errs = append(errs, &NotFoundError{BaseError: BaseError{
				Op: "RequireConfig", Scope: scope,
				Err: fmt.Errorf("no %s was found in the scope %s", r.level.describe(), strings.Join(unmet, ", ")),
			}})
		}
	}
	return errors.Join(errs...)
}

// qualifies reports whether e counts for a requirement at level, in scope, or anywhere when scope is empty.
func (ld *loading) qualifies(e *entry, level Require, scope ScopeName) bool {
	if e.err != nil || e.dropped {
		return false
	}
	everyScope := e.explicit && e.layer.Scope == "" && ld.c.configFilesOnly
	if scope != "" && e.layer.Scope != scope && !everyScope {
		return false
	}
	switch {
	case e.value:
		return e.layer.Status == LayerLoaded && len(e.tree) > 0 && level&RequireFile == 0
	case level&RequireFile != 0 && !e.file:
		return false
	case level&RequireNonEmpty != 0:
		return e.layer.Status == LayerLoaded
	default:
		return e.layer.Status == LayerLoaded || e.layer.Status == LayerEmpty
	}
}
