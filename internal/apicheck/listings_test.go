package apicheck_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// md turns three single quotes into a code fence, since Go's raw strings can't hold backquotes.
func md(s string) string { return strings.ReplaceAll(s, "'''", "```") }

// DL-256: a listing that matches the code has no findings, whatever its parameter names, grouping, comments, method
// bodies or partial structs.
func TestListingsMatch(t *testing.T) {
	t.Parallel()
	doc := md(`# Fixture

Prose with ` + "`Nothing`" + ` in it isn't checked.

'''go
// Loading
func New(appName string, options ...Option) (*Loader, error)
func (ldr *Loader) Load(c context.Context) (Tree, error) // comments are fine
func Open(f fs.FS) error
func WithMap(fn func(n, v string) (string, bool)) Option
func WithRule[Q PathSpec](p Q, count int) Option

type Option interface{ /* sealed */ }
type ScopeName string
func (s ScopeName) String() string { return string(s) }

const (
	ScopeProject ScopeName = "project"
	MaxDepth               = 8
)

const CodeNotFound Code = 404

var ErrParse error

type Layer struct {
	Path string // a partial struct: Scope and Size aren't listed
	hidden bool // unexported fields aren't checked
}

type Decoder interface {
	Format() string
	Decode(b []byte) (Tree, error)
}

type MapFunc func(string, string) (string, bool)
type Code int
type Tree map[string]any
type Alias = Layer

type FetchError struct {
	BaseError
	Timeout bool
}

func (e *FetchError) Error() string { return "" }
func (e *FetchError) Unwrap() error // promoted from BaseError

type Watcher[T any] struct{}
func (w *Watcher[T]) Current() T

func helper() // unexported, so not checked
'''
`)
	if got := check(t, doc); len(got) != 0 {
		t.Errorf("findings in a matching listing:\n%s", strings.Join(got, "\n"))
	}
}

// DL-256: each kind of difference is a finding at the declaration's line, naming the declaration.
func TestListingsDiffer(t *testing.T) {
	t.Parallel()
	doc := md(`'''go
func Nope() Option
func New(name int, opts ...Option) (*Loader, error)
func Open(fsys fs.FS) (bool, error)
func WithMap(f func(string) bool) Option
func WithRule[P ~string](path P, n int) Option
var Tree int
func ErrParse() error
const CodeNotFound Code = 403
const ScopeProject string = "project"
var ErrParse string
func (l *Loader) Load(ctx context.Context) (*Tree, error)
func (l Loader) Load(ctx context.Context) (Tree, error)
func (l *Loader) Close() error
func (n *Nobody) Close() error

type Layer struct {
	Size  int
	Owner string
}

type FetchError struct {
	Base
	Timeout bool
}

type Code string
type MapFunc func(string) bool
type Alias = Tree
type Tree Layer
type Watcher[T comparable] struct{}
'''
`)
	want := []string{
		fmt.Sprintf("%d . Nope: missing", at(t, doc, "func Nope")),
		fmt.Sprintf("%d . New: differs", at(t, doc, "func New")),
		fmt.Sprintf("%d . Open: differs", at(t, doc, "func Open")),
		fmt.Sprintf("%d . WithMap: differs", at(t, doc, "func WithMap")),
		fmt.Sprintf("%d . WithRule: differs", at(t, doc, "func WithRule")),
		fmt.Sprintf("%d . Tree: kind", at(t, doc, "var Tree")),
		fmt.Sprintf("%d . ErrParse: kind", at(t, doc, "func ErrParse")),
		fmt.Sprintf("%d . CodeNotFound: differs", at(t, doc, "const CodeNotFound")),
		fmt.Sprintf("%d . ScopeProject: differs", at(t, doc, "const ScopeProject")),
		fmt.Sprintf("%d . ErrParse: differs", at(t, doc, "var ErrParse")),
		fmt.Sprintf("%d . Loader.Load: differs", at(t, doc, "func (l *Loader) Load")),
		fmt.Sprintf("%d . Loader.Load: differs", at(t, doc, "func (l Loader) Load")),
		fmt.Sprintf("%d . Loader.Close: missing", at(t, doc, "func (l *Loader) Close")),
		fmt.Sprintf("%d . Nobody.Close: missing", at(t, doc, "func (n *Nobody) Close")),
		fmt.Sprintf("%d . Layer.Size: differs", at(t, doc, "Size  int")),
		fmt.Sprintf("%d . Layer.Owner: missing", at(t, doc, "Owner string")),
		fmt.Sprintf("%d . FetchError.Base: missing", at(t, doc, "\tBase\n")),
		fmt.Sprintf("%d . Code: differs", at(t, doc, "type Code")),
		fmt.Sprintf("%d . MapFunc: differs", at(t, doc, "type MapFunc")),
		fmt.Sprintf("%d . Alias: differs", at(t, doc, "type Alias")),
		fmt.Sprintf("%d . Tree: differs", at(t, doc, "type Tree")),
		fmt.Sprintf("%d . Watcher: differs", at(t, doc, "type Watcher")),
	}
	assertFindings(t, check(t, doc), want)
}

// DL-256: an interface listing names every exported method, and only methods the interface has.
func TestListingsInterfaces(t *testing.T) {
	t.Parallel()
	doc := md(`'''go
type Decoder interface {
	Decode(data []byte) (Tree, error)
}

type Option interface {
	Apply()
}
'''
`)
	want := []string{
		fmt.Sprintf("%d . Decoder.Format: incomplete", at(t, doc, "type Decoder")),
		fmt.Sprintf("%d . Option.Apply: missing", at(t, doc, "Apply()")),
	}
	assertFindings(t, check(t, doc), want)
}

// DL-256: a "// package" line sends what follows to each package it names, relative to the module, with "." for the
// core and an optional note in parentheses; an unknown package is a finding, and what follows it isn't checked.
func TestListingsPackageMarkers(t *testing.T) {
	t.Parallel()
	doc := md(`'''go
func Open(fsys fs.FS) error

// package sub (DL-1)
func New(opts ...Option) m.Decoder

// package sub, other
var Preset m.Option
func New(opts ...Option) m.Decoder

// package .
func New(name string, opts ...Option) (*Loader, error)

// package nope
func Anything()
'''

'''go
func Open(fsys fs.FS) error // a new block starts in the core again
'''
`)
	want := []string{
		fmt.Sprintf("%d other New: differs", at(t, doc, "func New(opts ...Option) m.Decoder\n\n// package .")),
		fmt.Sprintf("%d: unknown package", at(t, doc, "// package nope")),
	}
	assertFindings(t, check(t, doc), want)
}

// DL-256: a block that doesn't parse is a finding at its opening fence, superseded and example blocks are skipped, and
// so is any fence that isn't Go.
func TestListingsBlocks(t *testing.T) {
	t.Parallel()
	doc := md(`'''go
func Open(fsys fs.FS) error
Open(nil)
'''

<!-- api superseded DL-9 -->
'''go
func Gone() Option
'''

<!-- example -->
'''go
l, err := m.New("app")
'''

'''text
func Nope()
'''

'''go
func Nope2()
'''
`)
	want := []string{
		fmt.Sprintf("%d: unparsable", at(t, doc, "```go\nfunc Open")),
		fmt.Sprintf("%d . Nope2: missing", at(t, doc, "func Nope2")),
	}
	assertFindings(t, check(t, doc), want)
}

func assertFindings(t *testing.T, got, want []string) {
	t.Helper()
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("findings:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
