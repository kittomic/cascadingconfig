package yaml

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
	"github.com/goccy/go-yaml/token"

	"github.com/kittomic/cascadingconfig"
)

// Decoder returns the YAML decoder, for YAML 1.2 with the core schema.
//
// goccy/go-yaml parses the file, and the core schema's rules give unquoted values their types: yes, no, on and off are
// strings, 017 is 17 and 1e3 is a float. Only the core schema's tags are accepted. A key becomes a string by printing
// its resolved value, so 0x1F: becomes "31", and keys that clash are an error. Merge keys (<<) are supported, earlier
// entries winning. At most 10,000 values may come from aliases and merge keys. A file holds one document.
//
// Use it through cascadingconfig.DecodeDocument, which also strips a byte-order mark and rejects input that isn't
// UTF-8.
func Decoder(opts ...Option) cascadingconfig.Decoder {
	c := config{maxAliasValues: defaultMaxAliasValues}
	for _, o := range opts {
		if o != nil {
			o.apply(&c)
		}
	}
	return decoder{c: c}
}

const defaultMaxAliasValues = 10_000

// Option configures the YAML decoder.
type Option interface {
	apply(c *config)
}

type config struct {
	legacyNumbers  bool
	maxAliasValues int
}

type optionFunc func(c *config)

func (f optionFunc) apply(c *config) { f(c) }

// WithLegacyNumbers accepts the YAML 1.1 integer forms that goccy and yaml.v3 accept: 017 as octal, 0b11 as binary, and
// _ between digits.
func WithLegacyNumbers() Option {
	return optionFunc(func(c *config) { c.legacyNumbers = true })
}

// WithMaxAliasValues sets how many values aliases and merge keys may create in one document, 10,000 by default. A value
// below 1 makes every Decode fail, because Decoder has no error to return.
func WithMaxAliasValues(n int) Option {
	return optionFunc(func(c *config) { c.maxAliasValues = n })
}

type decoder struct {
	c config
}

func (decoder) Format() string { return "yaml" }

func (decoder) Extensions() []string { return []string{"yaml", "yml"} }

func (decoder) MediaTypes() []string {
	return []string{"application/yaml", "application/x-yaml", "text/yaml", "text/x-yaml"}
}

// Decode parses data with goccy and builds the tree from its syntax tree. Errors carry a position and a short message,
// never an excerpt of the file; a panic in goccy is returned as an error, so a hostile file can't crash the app.
func (d decoder) Decode(data []byte) (doc cascadingconfig.Document, err error) {
	defer func() {
		if r := recover(); r != nil {
			doc, err = cascadingconfig.Document{}, errors.New("the YAML parser failed on this file")
		}
	}()
	if d.c.maxAliasValues < 1 {
		return cascadingconfig.Document{}, fmt.Errorf("WithMaxAliasValues(%d): the cap must be at least 1", d.c.maxAliasValues)
	}
	f, err := parser.ParseBytes(data, 0)
	if err != nil {
		return cascadingconfig.Document{}, fromGoccy(err)
	}
	if len(f.Docs) > 1 {
		return cascadingconfig.Document{}, errorAt(docToken(f.Docs[1]), "the file holds more than one document; a config file must hold one")
	}
	if len(f.Docs) == 0 || f.Docs[0].Body == nil {
		return cascadingconfig.Document{Empty: true}, nil
	}
	b := &builder{c: d.c, anchors: map[string]anchor{}, building: map[string]bool{}}
	v, err := b.value(f.Docs[0].Body, "")
	if err != nil {
		return cascadingconfig.Document{}, err
	}
	switch v := v.(type) {
	case nil:
		return cascadingconfig.Document{Empty: true}, nil
	case map[string]any:
		return cascadingconfig.Document{Tree: v}, nil
	default:
		return cascadingconfig.Document{}, errorAt(f.Docs[0].Body.GetToken(), "the top level must be a table, not %s", describe(v))
	}
}

// docToken returns the token that starts a document, for its position.
func docToken(d *ast.DocumentNode) *token.Token {
	if d.Start != nil {
		return d.Start
	}
	if d.Body != nil {
		return d.Body.GetToken()
	}
	return nil
}

// posError is an error with a position, which DecodeDocument copies into its *ParseError.
type posError struct {
	msg          string
	line, column int
}

func (e *posError) Error() string                { return e.msg }
func (e *posError) Position() (line, column int) { return e.line, e.column }

func errorAt(tk *token.Token, format string, args ...any) error {
	e := &posError{msg: fmt.Sprintf(format, args...)}
	if tk != nil && tk.Position != nil {
		e.line, e.column = tk.Position.Line, tk.Position.Column
	}
	return e
}

// fromGoccy keeps goccy's short message and position, without its excerpt of the file.
func fromGoccy(err error) error {
	var ye yaml.Error
	if errors.As(err, &ye) {
		return errorAt(ye.GetToken(), "%s", ye.GetMessage())
	}
	return errors.New("the YAML parser failed on this file")
}

// anchor is an anchored node, with the tag it was written with.
type anchor struct {
	node ast.Node
	tag  string
}

// builder builds canonical values from goccy's syntax tree. Aliases are expanded by building their anchor's node again,
// so each value is a fresh copy, and every value built inside an alias or merge counts towards the cap.
type builder struct {
	c        config
	anchors  map[string]anchor
	building map[string]bool // anchors whose value is being built, to catch a self-alias
	inAlias  int
	aliasTok *token.Token // the outermost alias being expanded, for the cap's error
	created  int
}

// value builds n. tag is the tag written on it, normalised, or empty.
func (b *builder) value(n ast.Node, tag string) (any, error) {
	if b.inAlias > 0 {
		if b.created++; b.created > b.c.maxAliasValues {
			return nil, errorAt(b.aliasTok, "aliases and merge keys create more than %d values; the cap stops alias bombs", b.c.maxAliasValues)
		}
	}
	switch n := n.(type) {
	case *ast.TagNode:
		if tag != "" {
			return nil, errorAt(n.GetToken(), "a value has two tags")
		}
		t, err := normaliseTag(n)
		if err != nil {
			return nil, err
		}
		if n.Value == nil {
			return b.scalar(n.GetToken(), "", false, t)
		}
		if b.inAlias > 0 {
			b.created--
		}
		return b.value(n.Value, t)
	case *ast.AnchorNode:
		name := n.Name.GetToken().Value
		b.anchors[name] = anchor{node: n.Value, tag: tag}
		b.building[name] = true
		if b.inAlias > 0 {
			b.created--
		}
		v, err := b.value(n.Value, tag)
		delete(b.building, name)
		return v, err
	case *ast.AliasNode:
		name := n.Value.GetToken().Value
		a, ok := b.anchors[name]
		switch {
		case !ok:
			return nil, errorAt(n.GetToken(), "the alias *%s has no anchor before it", name)
		case b.building[name]:
			return nil, errorAt(n.GetToken(), "the alias *%s is inside its own anchor", name)
		case tag != "":
			return nil, errorAt(n.GetToken(), "an alias can't have a tag")
		}
		if b.inAlias == 0 {
			b.aliasTok = n.GetToken()
		} else {
			b.created-- // the alias itself isn't a value; what it expands to is
		}
		b.inAlias++
		v, err := b.value(a.node, a.tag)
		b.inAlias--
		return v, err
	case *ast.MappingNode:
		if err := wantTag(n, tag, "!!map"); err != nil {
			return nil, err
		}
		return b.mapping(n.Values)
	case *ast.MappingValueNode:
		if err := wantTag(n, tag, "!!map"); err != nil {
			return nil, err
		}
		return b.mapping([]*ast.MappingValueNode{n})
	case *ast.SequenceNode:
		if err := wantTag(n, tag, "!!seq"); err != nil {
			return nil, err
		}
		out := make([]any, 0, len(n.Values))
		for _, e := range n.Values {
			v, err := b.value(e, "")
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	case *ast.LiteralNode:
		return b.scalar(n.GetToken(), n.Value.Value, true, tag)
	case *ast.MappingKeyNode:
		return b.value(n.Value, tag)
	case *ast.MergeKeyNode:
		return b.scalar(n.GetToken(), "<<", false, tag)
	case *ast.NullNode:
		text := n.GetToken().Value
		if n.GetToken().Type == token.ImplicitNullType {
			text = ""
		}
		return b.scalar(n.GetToken(), text, false, tag)
	case *ast.StringNode:
		tk := n.GetToken()
		quoted := tk.Type == token.SingleQuoteType || tk.Type == token.DoubleQuoteType
		return b.scalar(tk, n.Value, quoted, tag)
	case ast.ScalarNode:
		return b.scalar(n.GetToken(), n.GetToken().Value, false, tag)
	default:
		return nil, errorAt(n.GetToken(), "unsupported YAML node %s", n.Type())
	}
}

// wantTag checks a collection's tag: none, the non-specific !, or want.
func wantTag(n ast.Node, tag, want string) error {
	if tag == "" || tag == "!" || tag == want {
		return nil
	}
	return errorAt(n.GetToken(), "the tag %s doesn't fit a %s", tag, map[string]string{"!!map": "table", "!!seq": "sequence"}[want])
}

// mapping builds a table: keys are resolved and printed, and clash if equal once printed; merge keys add the keys of
// the tables they name that the table doesn't set itself, earlier ones winning.
func (b *builder) mapping(pairs []*ast.MappingValueNode) (any, error) {
	out := make(map[string]any, len(pairs))
	var merges []map[string]any
	for _, p := range pairs {
		if isMergeKey(p.Key) {
			ms, err := b.merge(p)
			if err != nil {
				return nil, err
			}
			merges = append(merges, ms...)
			continue
		}
		kv, err := b.value(p.Key, "")
		if err != nil {
			return nil, err
		}
		key, err := keyString(p.Key, kv)
		if err != nil {
			return nil, err
		}
		if _, dup := out[key]; dup {
			return nil, errorAt(p.Key.GetToken(), "the key %q is set twice in one table", key)
		}
		v, err := b.value(p.Value, "")
		if err != nil {
			return nil, err
		}
		out[key] = v
	}
	for _, m := range merges {
		for k, v := range m {
			if _, set := out[k]; !set {
				out[k] = v
			}
		}
	}
	return out, nil
}

// isMergeKey reports whether k is the merge key: a plain, untagged <<.
func isMergeKey(k ast.MapKeyNode) bool {
	switch k := k.(type) {
	case *ast.MergeKeyNode:
		return true
	case *ast.StringNode:
		return k.Value == "<<" && k.GetToken().Type != token.SingleQuoteType && k.GetToken().Type != token.DoubleQuoteType
	}
	return false
}

// merge returns the tables a merge key names, in order: one table, or a sequence of them.
func (b *builder) merge(p *ast.MappingValueNode) ([]map[string]any, error) {
	v, err := b.value(p.Value, "")
	if err != nil {
		return nil, err
	}
	bad := errorAt(p.Key.GetToken(), "a merge key (<<) needs a table or a sequence of tables")
	switch v := v.(type) {
	case map[string]any:
		return []map[string]any{v}, nil
	case []any:
		out := make([]map[string]any, 0, len(v))
		for _, e := range v {
			m, ok := e.(map[string]any)
			if !ok {
				return nil, bad
			}
			out = append(out, m)
		}
		return out, nil
	default:
		return nil, bad
	}
}

// keyString prints a resolved key: strings as they are, null, booleans and numbers in canonical form.
func keyString(n ast.Node, v any) (string, error) {
	switch v := v.(type) {
	case string:
		return v, nil
	case nil:
		return "null", nil
	case bool:
		return strconv.FormatBool(v), nil
	case int64:
		return strconv.FormatInt(v, 10), nil
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64), nil
	default:
		return "", errorAt(n.GetToken(), "%s can't be a key; only a scalar can", describe(v))
	}
}

func describe(v any) string {
	switch v.(type) {
	case map[string]any:
		return "a table"
	case []any:
		return "a sequence"
	default:
		return "a scalar"
	}
}

// coreTags are the core schema's tags, by their short and full names.
var coreTags = map[string]string{"!": "!"}

func init() {
	for _, t := range []string{"str", "int", "float", "bool", "null", "map", "seq"} {
		coreTags["!!"+t] = "!!" + t
		coreTags["!<tag:yaml.org,2002:"+t+">"] = "!!" + t
	}
}

// normaliseTag returns a tag's short core-schema name, or an error for any other tag.
func normaliseTag(n *ast.TagNode) (string, error) {
	text := n.Start.Value
	if t, ok := coreTags[text]; ok && n.Directive == nil {
		return t, nil
	}
	name := text
	if strings.HasPrefix(text, "!<") && strings.HasSuffix(text, ">") {
		name = text[2 : len(text)-1]
	}
	return "", errorAt(n.GetToken(), "the tag %s isn't supported; only the YAML 1.2 core schema's tags are", name)
}

// The YAML 1.2 core schema's resolution (§10.3.2), and the YAML 1.1 integer forms WithLegacyNumbers adds.
var (
	coreNull   = regexp.MustCompile(`^(null|Null|NULL|~)?$`)
	coreBool   = regexp.MustCompile(`^(true|True|TRUE|false|False|FALSE)$`)
	coreInt10  = regexp.MustCompile(`^[-+]?[0-9]+$`)
	coreInt8   = regexp.MustCompile(`^0o[0-7]+$`)
	coreInt16  = regexp.MustCompile(`^0x[0-9a-fA-F]+$`)
	coreFloat  = regexp.MustCompile(`^[-+]?(\.[0-9]+|[0-9]+(\.[0-9]*)?)([eE][-+]?[0-9]+)?$`)
	coreInf    = regexp.MustCompile(`^[-+]?(\.inf|\.Inf|\.INF)$`)
	coreNaN    = regexp.MustCompile(`^(\.nan|\.NaN|\.NAN)$`)
	legacyInt2 = regexp.MustCompile(`^[-+]?0b[01_]+$`)
	legacyInt8 = regexp.MustCompile(`^[-+]?0[0-7_]+$`)
	legacyInt  = regexp.MustCompile(`^[-+]?(0|[1-9][0-9_]*)$`)
	legacyHex  = regexp.MustCompile(`^[-+]?0x[0-9a-fA-F_]+$`)
)

// scalar resolves a scalar's text. A quoted or block scalar is a string unless a tag says otherwise; a plain one gets
// its type from the core schema. A tag forces its type, and text that doesn't fit it is an error.
func (b *builder) scalar(tk *token.Token, text string, quoted bool, tag string) (any, error) {
	switch tag {
	case "":
		if quoted {
			return text, nil
		}
		return b.resolve(tk, text)
	case "!", "!!str":
		return text, nil
	case "!!null":
		if coreNull.MatchString(text) {
			return nil, nil
		}
	case "!!bool":
		if coreBool.MatchString(text) {
			return strings.EqualFold(text, "true"), nil
		}
	case "!!int":
		if v, ok, err := b.integer(tk, text); ok || err != nil {
			return v, err
		}
	case "!!float":
		if v, ok, err := b.integer(tk, text); ok || err != nil {
			if err != nil {
				return nil, err
			}
			return float64(v.(int64)), nil
		}
		if v, ok, err := float(tk, text); ok || err != nil {
			return v, err
		}
	case "!!map", "!!seq":
		return nil, errorAt(tk, "the tag %s doesn't fit a scalar", tag)
	}
	return nil, errorAt(tk, "the value doesn't fit its tag %s", tag)
}

// resolve gives a plain scalar its type.
func (b *builder) resolve(tk *token.Token, text string) (any, error) {
	switch {
	case coreNull.MatchString(text):
		return nil, nil
	case coreBool.MatchString(text):
		return strings.EqualFold(text, "true"), nil
	}
	if v, ok, err := b.integer(tk, text); ok || err != nil {
		return v, err
	}
	if v, ok, err := float(tk, text); ok || err != nil {
		return v, err
	}
	return text, nil
}

// integer parses text as an integer if it is one, and reports an error if it's out of the int64 range.
func (b *builder) integer(tk *token.Token, text string) (any, bool, error) {
	var digits string
	base := 10
	switch {
	case b.c.legacyNumbers && legacyInt2.MatchString(text):
		digits, base = signed(text, "0b"), 2
	case b.c.legacyNumbers && legacyHex.MatchString(text):
		digits, base = signed(text, "0x"), 16
	case b.c.legacyNumbers && legacyInt8.MatchString(text) && strings.Trim(text, "+-0_") != "":
		digits, base = signed(text, "0"), 8
	case b.c.legacyNumbers && legacyInt.MatchString(text):
		digits = text
	case coreInt10.MatchString(text):
		digits = text
	case coreInt8.MatchString(text):
		digits, base = text[2:], 8
	case coreInt16.MatchString(text):
		digits, base = text[2:], 16
	default:
		return nil, false, nil
	}
	v, err := strconv.ParseInt(strings.ReplaceAll(digits, "_", ""), base, 64)
	if err != nil {
		return nil, true, errorAt(tk, "the integer is outside the int64 range")
	}
	return v, true, nil
}

// signed moves a sign in front of a prefix: "-0x1F" with prefix "0x" gives "-1F".
func signed(text, prefix string) string {
	sign := ""
	if text[0] == '-' || text[0] == '+' {
		sign, text = text[:1], text[1:]
	}
	return sign + strings.TrimPrefix(text, prefix)
}

// float parses text as a float if it is one.
func float(tk *token.Token, text string) (any, bool, error) {
	switch {
	case coreInf.MatchString(text):
		if text[0] == '-' {
			return math.Inf(-1), true, nil
		}
		return math.Inf(1), true, nil
	case coreNaN.MatchString(text):
		return math.NaN(), true, nil
	case coreFloat.MatchString(text):
		f, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return nil, true, errorAt(tk, "the number is outside the float64 range")
		}
		return f, true, nil
	}
	return nil, false, nil
}
