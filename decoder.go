package cascadingconfig

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
	_, _, _ = d, path, data
	return Document{}, errNotImplemented
}
