package cascadingconfig

import (
	"bytes"
	"errors"
	"fmt"
	"unicode/utf8"
)

// Decoder parses one format. It is implemented by format/json, format/toml and format/yaml, and by any format an app
// adds.
//
// A decoder must produce only the canonical value types documented on Tree, and must reject a document whose top
// level isn't a map. The rules every format shares are applied around it by DecodeDocument.
type Decoder interface {
	// Format returns the format's name, such as "toml".
	Format() string
	// Extensions returns the file extensions the format uses, in rank order, highest first. For YAML that's "yaml",
	// then "yml".
	Extensions() []string
	// MediaTypes returns the media types the format uses, for http(s) includes without an extension.
	MediaTypes() []string
	// Decode decodes one document.
	Decode(data []byte) (Document, error)
}

// PositionError is implemented by decode errors that know where they happened. DecodeDocument copies the position
// into the *ParseError it returns.
type PositionError interface {
	error
	Position() (line, column int)
}

// Document is one decoded file.
type Document struct {
	Tree     Tree      // nil when Empty
	Empty    bool      // whitespace, comments only, or a null document; {} is not empty
	Warnings []Warning // non-fatal problems, such as a JSON duplicate key
}

// Warning is a non-fatal problem. Warnings go to the logger and the report.
type Warning struct {
	Code    WarningCode // what kind of problem
	Path    string      // the file; DecodeDocument fills it in when a decoder leaves it empty
	Key     Path        // where in the file, if known
	Message string
}

// WarningCode identifies a kind of warning.
type WarningCode string

// Warning codes. Later milestones add more.
const (
	// WarnDuplicateKey reports a key that appears more than once in one JSON object. The last value wins.
	WarnDuplicateKey WarningCode = "duplicate-key"

	// WarnFormatConflict reports a file skipped because a higher-ranked file in the same slot was loaded, under
	// ConflictFirst.
	WarnFormatConflict WarningCode = "format-conflict"
)

// DecodeDocument runs d on data, with the rules every format shares:
//
//   - a leading UTF-8 byte-order mark is stripped before d sees the input
//   - input that isn't valid UTF-8 is a *ParseError
//   - any error from d becomes a *ParseError, with the position if the error is a PositionError
//   - a non-canonical value type from d is a bug in d, reported as a *BaseError with CodeInternal
//   - warnings without a Path get path
//
// path is the file's path, used in errors and warnings.
func DecodeDocument(d Decoder, path string, data []byte) (Document, error) {
	data = bytes.TrimPrefix(data, byteOrderMark)
	if offset := invalidUTF8Offset(data); offset >= 0 {
		line, column := position(data, offset)
		return Document{}, &ParseError{
			BaseError: BaseError{Op: "decode", Path: path, Err: errors.New("the input isn't valid UTF-8")},
			Format:    d.Format(),
			Line:      line,
			Column:    column,
		}
	}

	doc, err := d.Decode(data)
	if err != nil {
		return Document{}, parseError(d, path, err)
	}

	if doc.Empty {
		doc.Tree = nil
	} else {
		if doc.Tree == nil {
			return Document{}, NewError(CodeInternal, "decode", path,
				fmt.Errorf("the %s decoder returned no tree for a non-empty document", d.Format()))
		}
		if err := checkCanonical(doc.Tree, nil); err != nil {
			return Document{}, NewError(CodeInternal, "decode", path,
				fmt.Errorf("the %s decoder produced %w", d.Format(), err))
		}
	}

	if len(doc.Warnings) > 0 {
		warnings := make([]Warning, len(doc.Warnings))
		copy(warnings, doc.Warnings)
		for i := range warnings {
			if warnings[i].Path == "" {
				warnings[i].Path = path
			}
		}
		doc.Warnings = warnings
	}
	return doc, nil
}

var byteOrderMark = []byte{0xEF, 0xBB, 0xBF}

// parseError turns a decoder's error into a *ParseError, keeping a *ParseError the decoder built itself.
func parseError(d Decoder, path string, err error) *ParseError {
	var pe *ParseError
	if errors.As(err, &pe) {
		if pe.Path == "" {
			pe.Path = path
		}
		if pe.Format == "" {
			pe.Format = d.Format()
		}
		return pe
	}
	pe = &ParseError{BaseError: BaseError{Op: "decode", Path: path, Err: err}, Format: d.Format()}
	var pos PositionError
	if errors.As(err, &pos) {
		pe.Line, pe.Column = pos.Position()
	}
	return pe
}

// invalidUTF8Offset returns the byte offset of the first invalid UTF-8 sequence in data, or -1.
func invalidUTF8Offset(data []byte) int {
	for i := 0; i < len(data); {
		r, size := utf8.DecodeRune(data[i:])
		if r == utf8.RuneError && size <= 1 {
			return i
		}
		i += size
	}
	return -1
}

// position returns the 1-based line and column of a byte offset in data. Columns count bytes.
func position(data []byte, offset int) (line, column int) {
	offset = min(max(offset, 0), len(data))
	before := data[:offset]
	line = bytes.Count(before, []byte{'\n'}) + 1
	column = offset - (bytes.LastIndexByte(before, '\n') + 1) + 1
	return line, column
}

// checkCanonical reports the first value in v that isn't one of the canonical types documented on Tree.
func checkCanonical(v any, at Path) error {
	switch v := v.(type) {
	case nil, bool, int64, float64, string:
		return nil
	case []any:
		for _, e := range v {
			if err := checkCanonical(e, at); err != nil {
				return err
			}
		}
		return nil
	case map[string]any:
		for k, e := range v {
			if err := checkCanonical(e, append(at[:len(at):len(at)], k)); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("a non-canonical value of type %T at %q", v, at.String())
	}
}
