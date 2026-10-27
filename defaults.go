package cascadingconfig

import (
	"encoding"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/kittomic/cascadingconfig/internal/tree"
)

// ScopeDefaults is the scope WithDefaults defines.
const ScopeDefaults ScopeName = "defaults"

// WithDefaults defines the defaults scope from v: a struct, a pointer to one, or a map with string keys. It is
// converted once, at New, with the decoding struct tag: only non-zero fields are included, and values become canonical,
// so a time.Duration becomes "5s", a time.Time its RFC 3339 text, and an encoding.TextMarshaler its text.
//
// The scope is always the lowest and always selected, with TrustSystem; WithoutScopes(ScopeDefaults) removes it.
// Config can clear a default with null or the unset key. A second WithDefaults replaces the first. A value that can't
// be converted is an *OptionError naming the field.
func WithDefaults(v any) Option {
	return optionFunc(func(c *config) {
		if s := c.scope(ScopeDefaults); s != nil {
			if _, ok := s.kind.(defaultsKind); ok {
				s.kind = defaultsKind{value: v}
				return
			}
			c.fail("WithDefaults", "the scope %q is already defined", ScopeDefaults)
			return
		}
		c.scopes = slices.Insert(c.scopes, 0, &scopeDef{name: ScopeDefaults, kind: defaultsKind{value: v}, trust: TrustSystem})
		c.selected[ScopeDefaults] = true
	})
}

// defaultsKind is the defaults scope's kind: the value WithDefaults was given, then the tree finish converts it to.
type defaultsKind struct {
	value any
	tree  Tree
}

func (defaultsKind) defaultTrust() TrustLevel { return TrustSystem }
func (defaultsKind) readsFiles() bool         { return false }
func (defaultsKind) check(*config)            {}

func (k defaultsKind) discover(ld *loading, s *scopeDef) {
	ld.addValues(s, tree.CopyMap(k.tree))
}

// finishDefaults converts the defaults with the decoding tag, now that every option has run, and keeps their scope the
// lowest.
func (c *config) finishDefaults() {
	i := slices.IndexFunc(c.scopes, func(s *scopeDef) bool {
		_, ok := s.kind.(defaultsKind)
		return ok
	})
	if i < 0 {
		return
	}
	s := c.scopes[i]
	t, err := defaultsTree(s.kind.(defaultsKind).value, c.tagName(), false)
	if err != nil {
		c.fail("WithDefaults", "%w", err)
		s.invalid = true
	}
	s.kind = defaultsKind{tree: t}
	c.scopes = slices.Insert(slices.Delete(c.scopes, i, i+1), 0, s)
}

// defaultsTree converts a defaults value to a tree.
func defaultsTree(v any, tag string, squash bool) (Tree, error) {
	rv := reflect.ValueOf(v)
	for rv.IsValid() && rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil, fmt.Errorf("the defaults are a nil %T", v)
		}
		rv = rv.Elem()
	}
	switch {
	case !rv.IsValid():
		return nil, fmt.Errorf("the defaults are nil")
	case rv.Kind() != reflect.Struct && rv.Kind() != reflect.Map:
		return nil, fmt.Errorf("the defaults must be a struct, a pointer to one, or a map with string keys, not %T", v)
	}
	cv := converter{tag: tag, squash: squash}
	out, err := cv.convert(rv, nil)
	if err != nil {
		return nil, err
	}
	m, _ := out.(map[string]any)
	if m == nil {
		m = Tree{}
	}
	return m, nil
}

// converter turns Go values into canonical values, reading struct tags as decoding does.
type converter struct {
	tag    string
	squash bool // squash every embedded struct, as DecoderConfig.Squash does
}

var durationType = reflect.TypeFor[time.Duration]()

// convert returns the canonical form of rv, which is at path at, for errors.
func (cv converter) convert(rv reflect.Value, at Path) (any, error) {
	if !rv.IsValid() {
		return nil, nil
	}
	if rv.Type() == durationType {
		return time.Duration(rv.Int()).String(), nil
	}
	if rv.CanInterface() {
		if m, ok := rv.Interface().(encoding.TextMarshaler); ok {
			if rv.Kind() == reflect.Pointer && rv.IsNil() {
				return nil, nil
			}
			text, err := m.MarshalText()
			if err != nil {
				return nil, fmt.Errorf("the field %s: %w", at, err)
			}
			return string(text), nil
		}
	}
	switch rv.Kind() {
	case reflect.Pointer, reflect.Interface:
		if rv.IsNil() {
			return nil, nil
		}
		return cv.convert(rv.Elem(), at)
	case reflect.Bool:
		return rv.Bool(), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return rv.Int(), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		if rv.Uint() > math.MaxInt64 {
			return nil, fmt.Errorf("the field %s is beyond the int64 range", at)
		}
		return int64(rv.Uint()), nil //nolint:gosec // checked above
	case reflect.Float32, reflect.Float64:
		return rv.Float(), nil
	case reflect.String:
		return rv.String(), nil
	case reflect.Slice, reflect.Array:
		if rv.Kind() == reflect.Slice && rv.IsNil() {
			return nil, nil
		}
		out := make([]any, rv.Len())
		for i := range rv.Len() {
			v, err := cv.convert(rv.Index(i), appendPath(at, fmt.Sprintf("%d", i)))
			if err != nil {
				return nil, err
			}
			out[i] = v
		}
		return out, nil
	case reflect.Map:
		if rv.Type().Key().Kind() != reflect.String {
			return nil, fmt.Errorf("the map at %s has %s keys; only string keys are allowed", describeAt(at), rv.Type().Key())
		}
		out := make(map[string]any, rv.Len())
		for it := rv.MapRange(); it.Next(); {
			k := it.Key().String()
			v, err := cv.convert(it.Value(), appendPath(at, k))
			if err != nil {
				return nil, err
			}
			out[k] = v
		}
		return out, nil
	case reflect.Struct:
		out := map[string]any{}
		if err := cv.fields(rv, at, out); err != nil {
			return nil, err
		}
		return out, nil
	default:
		return nil, fmt.Errorf("the field %s is a %s, which a config value can't be", at, rv.Kind())
	}
}

// fields adds a struct's non-zero fields to out, flattening squashed ones into it.
func (cv converter) fields(rv reflect.Value, at Path, out map[string]any) error {
	t := rv.Type()
	for i := range t.NumField() {
		f, fv := t.Field(i), rv.Field(i)
		if !f.IsExported() && !f.Anonymous {
			continue
		}
		name, opts, _ := strings.Cut(f.Tag.Get(cv.tag), ",")
		if name == "-" {
			continue
		}
		squash := slices.Contains(strings.Split(opts, ","), "squash") || cv.squash && f.Anonymous
		if squash {
			for fv.Kind() == reflect.Pointer && !fv.IsNil() {
				fv = fv.Elem()
			}
			if fv.Kind() == reflect.Struct {
				if err := cv.fields(fv, at, out); err != nil {
					return err
				}
				continue
			}
		}
		if !f.IsExported() || fv.IsZero() {
			continue
		}
		if name == "" {
			name = f.Name
		}
		v, err := cv.convert(fv, appendPath(at, name))
		if err != nil {
			return err
		}
		out[name] = v
	}
	return nil
}

func describeAt(at Path) string {
	if len(at) == 0 {
		return "the top level"
	}
	return at.String()
}
