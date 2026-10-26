package conformance

// MatchTOML compares got, a value in the TOML decoder's tree, with want, the same value in toml-test's typed JSON as
// encoding/json decodes it, and returns "" when they match, or why they don't (DL-270).
func MatchTOML(got, want any) string {
	_, _ = got, want // not implemented yet
	return ""
}

// MatchJSON compares got, a value in a decoder's tree, with want, the value a yaml-test-suite case's in.json holds, as
// encoding/json decodes it with UseNumber, and returns "" when they match, or why they don't (DL-270).
func MatchJSON(got, want any) string {
	_, _ = got, want // not implemented yet
	return ""
}

// Outcome is one case's line in an outcomes file (DL-271).
type Outcome struct {
	Category string
	Reason   string
}

// The categories an outcomes file uses.
const (
	Deviation = "deviation" // deliberate, citing its decision
	Failure   = "failure"   // a known library failure
	Accepted  = "accepted"  // a JSON i_ case the decoder accepts
	Rejected  = "rejected"  // a JSON i_ case the decoder rejects
)

// ReadOutcomes parses an outcomes file: one case per line, as "<case> <category> <reason>", with blank lines and lines
// starting with # ignored. An unknown category, a missing reason, or a case listed twice is an error.
func ReadOutcomes(data []byte) (map[string]Outcome, error) {
	_ = data // not implemented yet
	return nil, nil
}
