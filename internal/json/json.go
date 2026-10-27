// Package json parses JSON documents into canonical config trees. It doesn't import the core, so both the core's
// layouts and the format/json package can wrap it in a decoder (DL-95).
package json

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Result is one parsed document.
type Result struct {
	Tree       map[string]any // nil when Empty
	Empty      bool           // whitespace only, or null; {} is not empty
	Duplicates []Duplicate    // keys that appeared more than once in one object; the last value won
}

// Duplicate is one duplicate key.
type Duplicate struct {
	Key     []string // the key's path; nil inside an array, where it can't be expressed as a path
	Message string
}

// Parse parses strict RFC 8259 JSON: no comments and no trailing commas. A duplicate key keeps its last value and is
// reported in Duplicates. Integers are int64 when the literal has no fraction or exponent and fits; one out of range is
// an error. A whitespace-only or null document is empty, and the top level must otherwise be an object. Errors have a
// Position() (line, column int) method.
//
// It expects valid UTF-8 without a byte-order mark, which cascadingconfig.DecodeDocument ensures.
func Parse(data []byte) (Result, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return Result{Empty: true}, nil
	}

	p := &parser{data: data, dec: json.NewDecoder(bytes.NewReader(data))}
	p.dec.UseNumber()

	start := firstNonSpace(data)
	tok, err := p.token()
	if err != nil {
		return Result{}, err
	}
	switch tok {
	case nil:
		if err := p.end(); err != nil {
			return Result{}, err
		}
		return Result{Empty: true}, nil
	case json.Delim('{'):
	default:
		return Result{}, p.errorAt(start, errors.New("the top level must be an object"))
	}

	tree, err := p.object([]string{}, true)
	if err != nil {
		return Result{}, err
	}
	if err := p.end(); err != nil {
		return Result{}, err
	}
	return Result{Tree: tree, Duplicates: p.duplicates}, nil
}

// parser walks the token stream of one document.
type parser struct {
	data       []byte
	dec        *json.Decoder
	duplicates []Duplicate
}

// token reads the next token, turning encoding/json's errors into positioned ones.
func (p *parser) token() (json.Token, error) {
	tok, err := p.dec.Token()
	if err == nil {
		return tok, nil
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, p.errorAt(len(p.data), errors.New("unexpected end of input"))
	}
	return nil, p.syntaxError(err)
}

// syntaxError positions a syntax error from the token stream. The offset in a SyntaxError from Decoder.Token isn't
// reliable: it differs between Go versions, and is sometimes relative to the current value rather than the document.
// So the whole document is checked again with json.Unmarshal, whose SyntaxError.Offset is the number of bytes read,
// including the offending one, in every Go version checked (1.26 and 1.27; json.Compact differs on 1.26). Unmarshalling
// into a json.RawMessage only validates. This only runs when the document is already known to be invalid.
func (p *parser) syntaxError(err error) error {
	var syntax *json.SyntaxError
	if errors.As(json.Unmarshal(p.data, new(json.RawMessage)), &syntax) {
		if int(syntax.Offset) >= len(p.data) {
			return p.errorAt(len(p.data), syntax)
		}
		return p.errorAt(int(syntax.Offset)-1, syntax)
	}
	return p.errorAt(int(p.dec.InputOffset()), err)
}

// end checks that nothing but whitespace follows the document's single value.
func (p *parser) end() error {
	offset := int(p.dec.InputOffset())
	if _, err := p.dec.Token(); errors.Is(err, io.EOF) {
		return nil
	}
	return p.errorAt(offset+firstNonSpace(p.data[offset:]), errors.New("unexpected data after the top-level value"))
}

// object reads the members of an object whose '{' has been read. at is the object's path, and keyed is false inside
// an array, where a duplicate key's position can't be expressed as a Path.
func (p *parser) object(at []string, keyed bool) (map[string]any, error) {
	obj := map[string]any{}
	for p.dec.More() {
		tok, err := p.token()
		if err != nil {
			return nil, err
		}
		key, ok := tok.(string)
		if !ok {
			return nil, p.errorAt(int(p.dec.InputOffset()), errors.New("an object key must be a string"))
		}
		var keyPath []string
		if keyed {
			keyPath = append(at[:len(at):len(at)], key)
		}

		value, err := p.value(keyPath, keyed)
		if err != nil {
			return nil, err
		}
		if _, dup := obj[key]; dup {
			p.duplicates = append(p.duplicates, Duplicate{
				Key:     keyPath,
				Message: fmt.Sprintf("duplicate key %q: the last value wins", key),
			})
		}
		obj[key] = value
	}
	if _, err := p.token(); err != nil { // the closing '}'
		return nil, err
	}
	return obj, nil
}

// array reads the elements of an array whose '[' has been read.
func (p *parser) array() ([]any, error) {
	arr := []any{}
	for p.dec.More() {
		value, err := p.value(nil, false)
		if err != nil {
			return nil, err
		}
		arr = append(arr, value)
	}
	if _, err := p.token(); err != nil { // the closing ']'
		return nil, err
	}
	return arr, nil
}

// value reads one value. at and keyed describe where it sits, for duplicate-key warnings.
func (p *parser) value(at []string, keyed bool) (any, error) {
	tok, err := p.token()
	if err != nil {
		return nil, err
	}
	switch tok := tok.(type) {
	case json.Delim:
		if tok == '{' {
			return p.object(at, keyed)
		}
		return p.array()
	case json.Number:
		return p.number(tok)
	default: // nil, bool or string
		return tok, nil
	}
}

// number converts a number literal: int64 when it has no fraction or exponent, float64 otherwise.
func (p *parser) number(n json.Number) (any, error) {
	literal := string(n)
	start := int(p.dec.InputOffset()) - len(literal)
	if !strings.ContainsAny(literal, ".eE") {
		i, err := strconv.ParseInt(literal, 10, 64)
		if err != nil {
			return nil, p.errorAt(start, fmt.Errorf("the integer %s is outside the int64 range", literal))
		}
		return i, nil
	}
	f, err := strconv.ParseFloat(literal, 64)
	if err != nil {
		return nil, p.errorAt(start, fmt.Errorf("the number %s is outside the float64 range", literal))
	}
	return f, nil
}

// errorAt returns err with the position of a byte offset in the document.
func (p *parser) errorAt(offset int, err error) error {
	line, column := position(p.data, offset)
	return &positionError{err: err, line: line, column: column}
}

// positionError is a parse error that knows where it happened. It implements cascadingconfig.PositionError, so
// cascadingconfig.DecodeDocument copies its position into the *ParseError it returns.
type positionError struct {
	err          error
	line, column int
}

func (e *positionError) Error() string                { return e.err.Error() }
func (e *positionError) Unwrap() error                { return e.err }
func (e *positionError) Position() (line, column int) { return e.line, e.column }

// position returns the 1-based line and column of a byte offset in data. Columns count bytes.
func position(data []byte, offset int) (line, column int) {
	offset = min(max(offset, 0), len(data))
	before := data[:offset]
	line = bytes.Count(before, []byte{'\n'}) + 1
	column = offset - (bytes.LastIndexByte(before, '\n') + 1) + 1
	return line, column
}

// firstNonSpace returns the offset of the first byte in data that isn't JSON whitespace.
func firstNonSpace(data []byte) int {
	for i, c := range data {
		if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			return i
		}
	}
	return len(data)
}
