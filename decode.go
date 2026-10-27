package cascadingconfig

import (
	"context"
	"encoding"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-viper/mapstructure/v2"

	"github.com/kittomic/cascadingconfig/internal/tree"
)

// Load runs l.Load and decodes the result into a fresh T, which can be any type mapstructure decodes into. On failure
// it returns the zero T and the error; use Loader.Load when the report is needed.
func Load[T any](ctx context.Context, l *Loader) (T, error) {
	var zero T
	res, err := l.Load(ctx)
	if err != nil {
		return zero, err
	}
	var v T
	if err := res.Decode(&v); err != nil {
		return zero, err
	}
	return v, nil
}

// Decode decodes the merged tree into v, which must be a non-nil pointer, as koanf's Unmarshal does. Each key that
// fails is its own *DecodeError, joined. On a failed result, it returns the load's error again.
func (r *Result) Decode(v any) error {
	return r.decode(nil, v)
}

// DecodeAt decodes the subtree at path into v, as koanf's Unmarshal(path, …) does. A path that isn't in the tree is not
// an error, and v is left untouched. A path string that doesn't parse, or a v that isn't a non-nil pointer, is a
// *BaseError with CodeBadRequest. On a failed result, it returns the load's error again.
func DecodeAt[P PathSpec](r *Result, path P, v any) error {
	p, err := toPath(path)
	if err != nil {
		return NewError(CodeBadRequest, "decode", "", err)
	}
	return r.decode(p, v)
}

// defaultHooks are the decode hooks koanf uses, composed in koanf's order: day-suffix durations ("7d"), other duration
// strings, and encoding.TextUnmarshaler, which also parses RFC 3339 strings into time.Time.
func defaultHooks() mapstructure.DecodeHookFunc {
	return mapstructure.ComposeDecodeHookFunc(dayDurationHook, mapstructure.StringToTimeDurationHookFunc(), textUnmarshalerHook)
}

// DecodeHook converts a value while decoding: data, of type from, is about to fill a value of type to. It returns the
// value to use instead, or data unchanged when it has nothing to do (DL-278).
type DecodeHook func(from, to reflect.Type, data any) (any, error)

// WithDecodeHook adds a decode hook. Hooks run in the order they're added, before the default hooks for durations,
// day-suffix durations and encoding.TextUnmarshaler. A nil hook is an *OptionError.
func WithDecodeHook(h DecodeHook) Option {
	return optionFunc(func(c *config) {
		if h == nil {
			c.fail("WithDecodeHook", "a nil hook")
			return
		}
		c.hooks = append(c.hooks, h)
	})
}

// WithZeroFields makes a null clear a value already set in Go, such as a field of the value being decoded into or a
// map entry, as mapstructure's ZeroFields does. Without it, a null leaves the field as it was, as koanf does.
func WithZeroFields() Option {
	return optionFunc(func(c *config) { c.zeroFields = true })
}

// WithoutWeakTyping turns off the conversions koanf makes while decoding, such as a string "8080" into an int, so a
// value must already have the field's type.
func WithoutWeakTyping() Option {
	return optionFunc(func(c *config) { c.strictTypes = true })
}

// WithTag sets the struct tag that decoding and WithDefaults read, instead of koanf. An empty tag is an *OptionError.
func WithTag(tag string) Option {
	return optionFunc(func(c *config) {
		if tag == "" {
			c.fail("WithTag", "the tag can't be empty")
			return
		}
		c.tag = tag
	})
}

// WithFlatPaths decodes from flattened keys, such as "server.port", as koanf's FlatPaths does.
func WithFlatPaths() Option {
	return optionFunc(func(c *config) { c.flatPaths = true })
}

// UnknownKeys says what happens to keys in the tree that no field consumes.
type UnknownKeys int

// Unknown-key modes.
const (
	// UnknownKeysWarn logs each unknown key and records it in the report. It is the default.
	UnknownKeysWarn UnknownKeys = iota
	// UnknownKeysError makes each unknown key a *DecodeError with Unknown set.
	UnknownKeysError
	// UnknownKeysIgnore neither logs nor records them.
	UnknownKeysIgnore
)

// WithUnknownKeys sets what happens to unknown keys. An unknown mode is an *OptionError.
func WithUnknownKeys(u UnknownKeys) Option {
	return optionFunc(func(c *config) {
		if u < UnknownKeysWarn || u > UnknownKeysIgnore {
			c.fail("WithUnknownKeys", "unknown mode %d", int(u))
			return
		}
		c.unknownKeys = u
	})
}

// tagName returns the struct tag: WithTag's, or koanf.
func (c *config) tagName() string {
	if c.tag != "" {
		return c.tag
	}
	return "koanf"
}

// decoderConfig returns a fresh decoder config for one decode into v, as koanf builds it.
func (c *config) decoderConfig(v any, md *mapstructure.Metadata) *mapstructure.DecoderConfig {
	hooks := make([]mapstructure.DecodeHookFunc, 0, len(c.hooks)+1)
	for _, h := range c.hooks {
		hooks = append(hooks, mapstructure.DecodeHookFuncType(h))
	}
	dc := mapstructure.DecoderConfig{
		DecodeHook:       mapstructure.ComposeDecodeHookFunc(append(hooks, defaultHooks())...),
		WeaklyTypedInput: !c.strictTypes,
		ZeroFields:       c.zeroFields,
	}
	dc.Result, dc.TagName, dc.Metadata = v, c.tagName(), md
	return &dc
}

// decode decodes the subtree at at into v.
func (r *Result) decode(at Path, v any) error {
	if r == nil {
		return NewError(CodeBadRequest, "decode", "", errors.New("the result is nil"))
	}
	if r.err != nil {
		return r.err
	}
	if rv := reflect.ValueOf(v); rv.Kind() != reflect.Pointer || rv.IsNil() {
		return NewError(CodeBadRequest, "decode", "", fmt.Errorf("can't decode into %T: it must be a non-nil pointer", v))
	}
	var data any = r.tree
	for _, k := range at {
		m, ok := data.(map[string]any)
		if !ok {
			return nil
		}
		if data, ok = m[k]; !ok {
			return nil
		}
	}
	data = tree.Copy(data) // so the app's value never shares the result's tables
	if m, ok := data.(map[string]any); ok && r.cfg.flatPaths {
		data = flatten(m)
	}

	md := &mapstructure.Metadata{}
	d, err := mapstructure.NewDecoder(r.cfg.decoderConfig(v, md))
	if err != nil {
		return NewError(CodeBadRequest, "decode", "", err)
	}
	var errs []error
	if err := d.Decode(data); err != nil {
		for _, e := range leafErrors(err) {
			errs = append(errs, r.decodeError(at, e))
		}
	}
	errs = append(errs, r.unknown(at, md.Unused)...)
	return errors.Join(errs...)
}

// leafErrors splits mapstructure's error into one per key: it wraps a joined error in a single message, and joins
// each struct's field errors.
func leafErrors(err error) []error {
	parts := Errors(err)
	if len(parts) > 1 {
		var out []error
		for _, e := range parts {
			out = append(out, leafErrors(e)...)
		}
		return out
	}
	if _, ok := err.(*mapstructure.DecodeError); !ok { //nolint:errorlint // a key's own error is a leaf; only wrappers are unwrapped
		if u := errors.Unwrap(err); u != nil {
			return leafErrors(u)
		}
	}
	return parts
}

// flatten turns nested tables into dotted keys, keeping empty tables, as koanf's maps.Flatten does.
func flatten(m map[string]any) map[string]any {
	out := map[string]any{}
	var walk func(m map[string]any, prefix string)
	walk = func(m map[string]any, prefix string) {
		for k, v := range m {
			if t, ok := v.(map[string]any); ok && len(t) > 0 {
				walk(t, prefix+k+".")
				continue
			}
			out[prefix+k] = v
		}
	}
	walk(m, "")
	return out
}

// decodeError turns one of mapstructure's errors into a *DecodeError for the key it names, with a message that never
// quotes the value.
func (r *Result) decodeError(at Path, err error) error {
	name := ""
	var me *mapstructure.DecodeError
	if errors.As(err, &me) {
		name = me.Name()
	}
	src := r.source(at, name)
	de := &DecodeError{
		BaseError: BaseError{Op: "decode", Path: src.layer.Path, Scope: string(src.layer.Scope), Err: err},
		Key:       src.key,
		source:    src.describe(),
	}
	var ute *mapstructure.UnconvertibleTypeError
	var pe *mapstructure.ParseError
	switch {
	case errors.As(err, &ute):
		de.detail = fmt.Sprintf("expected %s, got %s", ute.Expected.Type(), describeType(src.value))
	case errors.As(err, &pe):
		de.detail = fmt.Sprintf("expected %s, got %s", pe.Expected.Type(), describeType(src.value))
	case src.found:
		de.detail = "can't decode " + describeType(src.value)
	default:
		de.detail = "can't decode the value"
	}
	return de
}

// unknown handles the unknown keys of one decode: recorded and logged, or errors, or nothing.
func (r *Result) unknown(at Path, names []string) []error {
	mode := r.cfg.unknownKeys
	if mode == UnknownKeysIgnore || len(names) == 0 {
		return nil
	}
	names = r.leafKeys(at, names)
	var errs []error
	logger := r.cfg.log()
	for _, name := range names {
		src := r.source(at, name)
		if mode == UnknownKeysError {
			errs = append(errs, &DecodeError{
				BaseError: BaseError{Op: "decode", Path: src.layer.Path, Scope: string(src.layer.Scope), Err: errors.New("no field matches the key")},
				Key:       src.key, Unknown: true, source: src.describe(), detail: "matches no field",
			})
			continue
		}
		w := Warning{
			Code: WarnUnknownKey, Path: src.layer.Path, Key: src.key,
			Message: fmt.Sprintf("%s (set by %s) matches no field", src.key, src.describe()),
		}
		logger.LogAttrs(context.Background(), levelOf(w.Code), w.Message,
			slog.String("code", string(w.Code)), slog.String("path", src.describe()))
		r.mu.Lock()
		if !slices.ContainsFunc(r.unknownKeys, func(u Warning) bool { return slices.Equal(u.Key, w.Key) }) {
			r.unknownKeys = append(r.unknownKeys, w)
		}
		r.mu.Unlock()
	}
	return errs
}

// leafKeys expands each unused key that holds a non-empty table into the keys of its leaves, which may each come
// from a different file, sorted.
func (r *Result) leafKeys(at Path, names []string) []string {
	var out []string
	var expand func(name string, v any)
	expand = func(name string, v any) {
		m, ok := v.(map[string]any)
		if !ok || len(m) == 0 {
			out = append(out, name)
			return
		}
		for k, e := range m {
			expand(name+"."+k, e)
		}
	}
	for _, name := range names {
		src := r.source(at, name)
		expand(name, src.value)
	}
	slices.Sort(out)
	return out
}

// keySource is where a key mapstructure named came from.
type keySource struct {
	key    Path  // the key, with array indexes and map keys in brackets as mapstructure writes them
	layer  Layer // the layer that set the value
	envVar string
	value  any // the value in the tree, if found
	found  bool
}

// describe names the source for messages: the file, the variable, the defaults, or the scope.
func (s keySource) describe() string {
	switch {
	case s.layer.Path != "":
		return s.layer.Path
	case s.envVar != "":
		return s.envVar
	case s.layer.Scope == ScopeDefaults:
		return "defaults"
	case s.layer.Scope != "":
		return fmt.Sprintf("the %s scope", s.layer.Scope)
	default:
		return "an unknown layer"
	}
}

var bracket = regexp.MustCompile(`^\[([^\]]*)\]`)

// source resolves name, a key as mapstructure writes it relative to the subtree at at, against the tree, so keys with
// dots and keys that differ in case still resolve. The layer is the deepest one recorded: an array element's own
// layer, or the latest layer of a table.
func (r *Result) source(at Path, name string) keySource {
	var v any = r.tree
	n := r.origins
	var src keySource
	var keys Path // table keys only, for origins and env vars
	note := func() {
		if n != nil && n.set {
			src.layer = r.layers[n.layer]
			src.envVar = r.envVar(n.layer, keys)
		}
	}
	step := func(k string) {
		keys = append(keys, k)
		if n != nil {
			n = n.children[k]
		}
		note()
	}
	for _, k := range at {
		m, _ := v.(map[string]any)
		v = m[k]
		src.key = append(src.key, k)
		step(k)
	}
	inArray, missing := false, false
	rest := name
	for rest != "" {
		if m := bracket.FindStringSubmatch(rest); m != nil {
			rest = rest[len(m[0]):]
			if len(src.key) == 0 {
				src.key = Path{""}
			}
			src.key[len(src.key)-1] += m[0]
			switch t := v.(type) {
			case []any:
				i, err := strconv.Atoi(m[1])
				if err != nil || i < 0 || i >= len(t) {
					v, missing = nil, true
					continue
				}
				if n != nil && n.set && !inArray {
					src.layer = r.layers[n.elementLayers(len(t))[i]]
					src.envVar = ""
				}
				v, n, inArray = t[i], nil, true
			case map[string]any:
				k := matchKey(t, m[1], true)
				v = t[k]
				if !inArray {
					step(k)
				}
			default:
				v, missing = nil, true
			}
			continue
		}
		rest = strings.TrimPrefix(rest, ".")
		var k string
		if m, ok := v.(map[string]any); ok {
			k = matchKey(m, rest, false)
		}
		if k == "" {
			k, _, _ = strings.Cut(rest, ".")
			if i := strings.IndexByte(k, '['); i > 0 {
				k = k[:i]
			}
			v, missing = nil, true
		} else {
			v = v.(map[string]any)[k]
		}
		rest = rest[len(k):]
		src.key = append(src.key, k)
		if !inArray {
			step(k)
		}
	}
	src.value, src.found = v, !missing
	return src
}

// matchKey returns the key of m that rest starts with, followed by the end, a dot or a bracket, preferring an exact
// match, then the longest. With whole, the key must be all of rest. It returns "" if none matches.
func matchKey(m map[string]any, rest string, whole bool) string {
	best, bestFold := "", ""
	for k := range m {
		if len(k) > len(rest) || whole && len(k) != len(rest) {
			continue
		}
		if len(k) < len(rest) && rest[len(k)] != '.' && rest[len(k)] != '[' {
			continue
		}
		switch {
		case rest[:len(k)] == k && len(k) > len(best):
			best = k
		case strings.EqualFold(rest[:len(k)], k) && len(k) > len(bestFold):
			bestFold = k
		}
	}
	if best != "" {
		return best
	}
	return bestFold
}

// dayDurationHook turns day suffixes ("7d", "1.5d") into hours before a string becomes a time.Duration, as koanf's
// transformDurationStrHook does.
func dayDurationHook(from, to reflect.Type, data any) (any, error) {
	if from.Kind() != reflect.String || to != reflect.TypeFor[time.Duration]() {
		return data, nil
	}
	return expandDays(reflect.ValueOf(data).String()), nil
}

var dayDuration = regexp.MustCompile(`(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)d`)

// expandDays replaces each number of days with the same number of hours, times 24, exactly, in decimal.
func expandDays(s string) string {
	if !strings.Contains(s, "d") {
		return s
	}
	return dayDuration.ReplaceAllStringFunc(s, func(day string) string {
		digits := []byte("00" + day[:len(day)-1])
		carry := 0
		for i := len(digits) - 1; i >= 0; i-- {
			if digits[i] == '.' {
				continue
			}
			n := int(digits[i]-'0')*24 + carry
			digits[i], carry = byte(n%10)+'0', n/10
		}
		return string(digits) + "h"
	})
}

// textUnmarshalerHook decodes a string into any type whose pointer implements encoding.TextUnmarshaler, including
// string types, as koanf's textUnmarshalerHookFunc does. A string type that is also a TextMarshaler is passed its
// marshaled text.
func textUnmarshalerHook(from, to reflect.Type, data any) (any, error) {
	if from.Kind() != reflect.String {
		return data, nil
	}
	result := reflect.New(to).Interface()
	u, ok := result.(encoding.TextUnmarshaler)
	if !ok {
		return data, nil
	}
	dv := reflect.ValueOf(data)
	text := []byte(dv.String())
	if from.Kind() == to.Kind() {
		ptr := reflect.New(dv.Type())
		ptr.Elem().Set(dv)
		for _, x := range []reflect.Value{dv, ptr} {
			if m, ok := x.Interface().(encoding.TextMarshaler); ok {
				var err error
				if text, err = m.MarshalText(); err != nil {
					return nil, err
				}
				break
			}
		}
	}
	if err := u.UnmarshalText(text); err != nil {
		return nil, err
	}
	return result, nil
}
