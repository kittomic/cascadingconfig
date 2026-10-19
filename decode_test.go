package cascadingconfig_test

import (
	"errors"
	"reflect"
	"testing"
	"time"

	cc "github.com/kittomic/cascadingconfig"
)

// fakeDecoder returns a fixed Document or error, and records the input it was given.
type fakeDecoder struct {
	doc    cc.Document
	err    error
	called bool
	got    []byte
}

func (d *fakeDecoder) Format() string       { return "fake" }
func (d *fakeDecoder) Extensions() []string { return []string{"fake"} }
func (d *fakeDecoder) MediaTypes() []string { return []string{"application/x-fake"} }

func (d *fakeDecoder) Decode(data []byte) (cc.Document, error) {
	d.called = true
	d.got = append([]byte(nil), data...)
	return d.doc, d.err
}

// positionError is a decoder error that knows where it happened.
type positionError struct{ line, column int }

func (e positionError) Error() string                { return "unexpected token" }
func (e positionError) Position() (line, column int) { return e.line, e.column }

// DL-52, DL-82: a leading UTF-8 byte-order mark is stripped before the decoder sees the input.
func TestDecodeDocumentStripsByteOrderMark(t *testing.T) {
	d := &fakeDecoder{doc: cc.Document{Tree: cc.Tree{}}}
	if _, err := cc.DecodeDocument(d, "app.fake", []byte("\xEF\xBB\xBF{}")); err != nil {
		t.Fatalf("DecodeDocument error: %v", err)
	}
	if string(d.got) != "{}" {
		t.Errorf("decoder got %q, want %q", d.got, "{}")
	}
}

// DL-52, DL-82: input that isn't UTF-8 is a *ParseError, and the decoder is never called.
func TestDecodeDocumentRejectsInvalidUTF8(t *testing.T) {
	d := &fakeDecoder{doc: cc.Document{Tree: cc.Tree{}}}
	_, err := cc.DecodeDocument(d, "app.fake", []byte("{\"a\": \"\xff\"}"))
	var pe *cc.ParseError
	if !errors.As(err, &pe) {
		t.Fatalf("error = %v, want a *ParseError", err)
	}
	if pe.Path != "app.fake" || pe.Format != "fake" {
		t.Errorf("Path, Format = %q, %q; want \"app.fake\", \"fake\"", pe.Path, pe.Format)
	}
	if d.called {
		t.Errorf("the decoder was called with input that isn't UTF-8")
	}
}

// DL-82: a decoder error with a position becomes a *ParseError carrying that position, the file, the format, and the
// original error as its cause.
func TestDecodeDocumentWrapsPositionedErrors(t *testing.T) {
	cause := positionError{line: 3, column: 8}
	_, err := cc.DecodeDocument(&fakeDecoder{err: cause}, "app.fake", []byte("x"))
	var pe *cc.ParseError
	if !errors.As(err, &pe) {
		t.Fatalf("error = %v, want a *ParseError", err)
	}
	if pe.Line != 3 || pe.Column != 8 {
		t.Errorf("Line, Column = %d, %d; want 3, 8", pe.Line, pe.Column)
	}
	if pe.Path != "app.fake" || pe.Format != "fake" {
		t.Errorf("Path, Format = %q, %q; want \"app.fake\", \"fake\"", pe.Path, pe.Format)
	}
	if !errors.As(err, new(positionError)) {
		t.Errorf("the *ParseError doesn't unwrap to the decoder's error")
	}
	if cc.CodeOf(err) != cc.CodeBadRequest {
		t.Errorf("CodeOf = %d, want %d", cc.CodeOf(err), cc.CodeBadRequest)
	}
}

// DL-82: a decoder error without a position becomes a *ParseError with no position.
func TestDecodeDocumentWrapsPlainErrors(t *testing.T) {
	cause := errors.New("bad document")
	_, err := cc.DecodeDocument(&fakeDecoder{err: cause}, "app.fake", []byte("x"))
	var pe *cc.ParseError
	if !errors.As(err, &pe) {
		t.Fatalf("error = %v, want a *ParseError", err)
	}
	if pe.Line != 0 || pe.Column != 0 {
		t.Errorf("Line, Column = %d, %d; want 0, 0", pe.Line, pe.Column)
	}
	if !errors.Is(err, cause) {
		t.Errorf("the *ParseError doesn't unwrap to the decoder's error")
	}
}

// DL-82: only canonical value types may come out of a decoder. Anything else is a decoder bug: a 500, not a parse
// error.
func TestDecodeDocumentRejectsNonCanonicalTypes(t *testing.T) {
	tests := []struct {
		name string
		tree cc.Tree
	}{
		{"int", cc.Tree{"a": 1}},
		{"uint64", cc.Tree{"a": uint64(1)}},
		{"float32", cc.Tree{"a": float32(1)}},
		{"time.Time", cc.Tree{"a": time.Unix(0, 0)}},
		{"[]string", cc.Tree{"a": []string{"x"}}},
		{"map[string]int", cc.Tree{"a": map[string]int{"x": 1}}},
		{"map[any]any", cc.Tree{"a": map[any]any{"x": "y"}}},
		{"nested in an array", cc.Tree{"a": []any{int32(1)}}},
		{"nested in a map", cc.Tree{"a": map[string]any{"b": map[string]any{"c": 1}}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := cc.DecodeDocument(&fakeDecoder{doc: cc.Document{Tree: tc.tree}}, "app.fake", []byte("x"))
			if got := cc.CodeOf(err); got != cc.CodeInternal {
				t.Errorf("CodeOf = %d, want %d (error %v)", got, cc.CodeInternal, err)
			}
			if errors.Is(err, cc.ErrParse) {
				t.Errorf("a non-canonical type was reported as a parse error")
			}
		})
	}
}

// api.md Core types: every canonical value type passes through unchanged.
func TestDecodeDocumentAcceptsCanonicalTypes(t *testing.T) {
	tree := cc.Tree{
		"null":   nil,
		"bool":   true,
		"int":    int64(42),
		"float":  1.5,
		"string": "x",
		"array":  []any{nil, false, int64(1), 2.5, "y", []any{}, map[string]any{}},
		"map":    map[string]any{"nested": map[string]any{"deep": "z"}},
	}
	doc, err := cc.DecodeDocument(&fakeDecoder{doc: cc.Document{Tree: tree}}, "app.fake", []byte("x"))
	if err != nil {
		t.Fatalf("DecodeDocument error: %v", err)
	}
	if !reflect.DeepEqual(doc.Tree, tree) {
		t.Errorf("Tree = %#v, want %#v", doc.Tree, tree)
	}
}

// DL-81: an empty document passes through as empty, with no tree.
func TestDecodeDocumentEmpty(t *testing.T) {
	doc, err := cc.DecodeDocument(&fakeDecoder{doc: cc.Document{Empty: true}}, "app.fake", []byte("   "))
	if err != nil {
		t.Fatalf("DecodeDocument error: %v", err)
	}
	if !doc.Empty || doc.Tree != nil {
		t.Errorf("Empty, Tree = %v, %#v; want true, nil", doc.Empty, doc.Tree)
	}
}

// DL-83: warnings without a file path get the document's path; warnings that have one keep it.
func TestDecodeDocumentFillsWarningPaths(t *testing.T) {
	d := &fakeDecoder{doc: cc.Document{
		Tree: cc.Tree{},
		Warnings: []cc.Warning{
			{Code: cc.WarnDuplicateKey, Key: cc.Path{"a"}},
			{Code: cc.WarnDuplicateKey, Key: cc.Path{"b"}, Path: "other.fake"},
		},
	}}
	doc, err := cc.DecodeDocument(d, "app.fake", []byte("x"))
	if err != nil {
		t.Fatalf("DecodeDocument error: %v", err)
	}
	if len(doc.Warnings) != 2 {
		t.Fatalf("got %d warnings, want 2", len(doc.Warnings))
	}
	if doc.Warnings[0].Path != "app.fake" {
		t.Errorf("first warning Path = %q, want \"app.fake\"", doc.Warnings[0].Path)
	}
	if doc.Warnings[1].Path != "other.fake" {
		t.Errorf("second warning Path = %q, want \"other.fake\"", doc.Warnings[1].Path)
	}
}
