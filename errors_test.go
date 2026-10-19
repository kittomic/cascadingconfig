package cascadingconfig_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	cc "github.com/kittomic/cascadingconfig"
)

// errorCase describes one specific error type: how to build it, its sentinel, its code, and how to match it with
// errors.As.
type errorCase struct {
	name     string
	err      error
	sentinel error
	code     cc.Code
	as       func(error) bool
}

func asType[T error](err error) bool {
	var target T
	return errors.As(err, &target)
}

var errCause = errors.New("boom")

// base is the BaseError every case embeds. Its exported fields are what other modules use to build errors (DL-84).
func base() cc.BaseError {
	return cc.BaseError{Op: "load", Path: "app.json", Err: errCause}
}

func errorCases() []errorCase {
	return []errorCase{
		{"OptionError", &cc.OptionError{BaseError: base()}, cc.ErrInvalidOption, 400, asType[*cc.OptionError]},
		{"ParseError", &cc.ParseError{BaseError: base()}, cc.ErrParse, 400, asType[*cc.ParseError]},
		{"ReferenceError", &cc.ReferenceError{BaseError: base()}, cc.ErrInvalidReference, 400, asType[*cc.ReferenceError]},
		{"PolicyError", &cc.PolicyError{BaseError: base()}, cc.ErrPolicy, 403, asType[*cc.PolicyError]},
		{"NotFoundError", &cc.NotFoundError{BaseError: base()}, cc.ErrNotFound, 404, asType[*cc.NotFoundError]},
		{"ConflictError", &cc.ConflictError{BaseError: base()}, cc.ErrConflict, 409, asType[*cc.ConflictError]},
		{"TooLargeError", &cc.TooLargeError{BaseError: base()}, cc.ErrTooLarge, 413, asType[*cc.TooLargeError]},
		{
			"UnsupportedFormatError", &cc.UnsupportedFormatError{BaseError: base()}, cc.ErrUnsupportedFormat, 415,
			asType[*cc.UnsupportedFormatError],
		},
		{"DecodeError", &cc.DecodeError{BaseError: base()}, cc.ErrDecode, 422, asType[*cc.DecodeError]},
		{"MergeError", &cc.MergeError{BaseError: base()}, cc.ErrMerge, 422, asType[*cc.MergeError]},
		{"TransformError", &cc.TransformError{BaseError: base()}, cc.ErrTransform, 422, asType[*cc.TransformError]},
		{"IOError", &cc.IOError{BaseError: base()}, cc.ErrIO, 500, asType[*cc.IOError]},
		{
			"UnsupportedSchemeError", &cc.UnsupportedSchemeError{BaseError: base()}, cc.ErrUnsupportedScheme, 501,
			asType[*cc.UnsupportedSchemeError],
		},
		{"FetchError", &cc.FetchError{BaseError: base()}, cc.ErrFetch, 502, asType[*cc.FetchError]},
		{"FetchError timeout", &cc.FetchError{BaseError: base(), Timeout: true}, cc.ErrFetch, 504, asType[*cc.FetchError]},
		{"CycleError", &cc.CycleError{BaseError: base()}, cc.ErrIncludeCycle, 508, asType[*cc.CycleError]},
	}
}

// api.md Errors, DL-84: every error type has its documented code and sentinel, matches with errors.Is and errors.As,
// and unwraps to its cause, even when wrapped again by the caller.
func TestErrorTypes(t *testing.T) {
	cases := errorCases()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wrapped := fmt.Errorf("caller: %w", tc.err)

			if got := cc.CodeOf(wrapped); got != tc.code {
				t.Errorf("CodeOf = %d, want %d", got, tc.code)
			}
			var coder cc.Coder
			if !errors.As(tc.err, &coder) || coder.Code() != tc.code {
				t.Errorf("as a Coder, Code() = %v, want %d", coder, tc.code)
			}
			if !errors.Is(wrapped, tc.sentinel) {
				t.Errorf("errors.Is(err, %v) = false, want true", tc.sentinel)
			}
			if !tc.as(wrapped) {
				t.Errorf("errors.As to %s = false, want true", tc.name)
			}
			if !errors.Is(wrapped, errCause) {
				t.Errorf("errors.Is(err, cause) = false: the error doesn't unwrap to its cause")
			}
			for _, other := range cases {
				if !errors.Is(other.sentinel, tc.sentinel) && errors.Is(tc.err, other.sentinel) {
					t.Errorf("errors.Is(err, %v) = true, want false", other.sentinel)
				}
			}
		})
	}
}

// api.md Errors: the message format of the base error and of a ParseError with a position.
func TestErrorMessages(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"with a path", cc.NewError(cc.CodeInternal, "load", "app.json", errCause), "cascadingconfig: load app.json: boom"},
		{"without a path", cc.NewError(cc.CodeInternal, "load", "", errCause), "cascadingconfig: load: boom"},
		{"a specific type", &cc.PolicyError{BaseError: base()}, "cascadingconfig: load app.json: boom"},
		{
			"a ParseError with a position",
			&cc.ParseError{
				BaseError: cc.BaseError{Op: "decode", Path: "app.json", Err: errCause}, Format: "json", Line: 3, Column: 5,
			},
			"cascadingconfig: decode app.json:3:5: boom",
		},
		{
			"a ParseError without a position",
			&cc.ParseError{BaseError: cc.BaseError{Op: "decode", Path: "app.json", Err: errCause}, Format: "json"},
			"cascadingconfig: decode app.json: boom",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.err.Error(); got != tc.want {
				t.Errorf("Error() = %q, want %q", got, tc.want)
			}
		})
	}
}

// api.md Errors: NewError builds base errors with any code, for situations with no specific type.
func TestNewError(t *testing.T) {
	err := cc.NewError(cc.CodeInternal, "trust", "app.json", errCause)
	if got := cc.CodeOf(err); got != cc.CodeInternal {
		t.Errorf("CodeOf = %d, want %d", got, cc.CodeInternal)
	}
	if !errors.Is(err, errCause) {
		t.Errorf("errors.Is(err, cause) = false, want true")
	}
	if err.Op != "trust" || err.Path != "app.json" {
		t.Errorf("Op, Path = %q, %q; want \"trust\", \"app.json\"", err.Op, err.Path)
	}
}

// DL-75: a cancelled or timed-out load keeps errors.Is(err, context.Canceled) and context.DeadlineExceeded working,
// with codes 499 and 504.
func TestContextErrors(t *testing.T) {
	canceled := cc.NewError(cc.CodeCanceled, "load", "", context.Canceled)
	if !errors.Is(canceled, context.Canceled) {
		t.Errorf("errors.Is(err, context.Canceled) = false, want true")
	}
	if got := cc.CodeOf(canceled); got != 499 {
		t.Errorf("CodeOf(canceled) = %d, want 499", got)
	}

	deadline := cc.NewError(cc.CodeGatewayTimeout, "load", "", context.DeadlineExceeded)
	if !errors.Is(deadline, context.DeadlineExceeded) {
		t.Errorf("errors.Is(err, context.DeadlineExceeded) = false, want true")
	}
	if got := cc.CodeOf(deadline); got != 504 {
		t.Errorf("CodeOf(deadline) = %d, want 504", got)
	}
}

// api.md Errors: the code constants have their documented values.
func TestCodeConstants(t *testing.T) {
	want := map[cc.Code]int{
		cc.CodeBadRequest:           400,
		cc.CodeForbidden:            403,
		cc.CodeNotFound:             404,
		cc.CodeConflict:             409,
		cc.CodeTooLarge:             413,
		cc.CodeUnsupportedMediaType: 415,
		cc.CodeUnprocessable:        422,
		cc.CodeCanceled:             499,
		cc.CodeInternal:             500,
		cc.CodeNotImplemented:       501,
		cc.CodeBadGateway:           502,
		cc.CodeGatewayTimeout:       504,
		cc.CodeLoopDetected:         508,
	}
	for code, value := range want {
		if int(code) != value {
			t.Errorf("code constant = %d, want %d", code, value)
		}
	}
}

// api.md Errors: CodeOf with no cascadingconfig error in the chain.
func TestCodeOfWithoutCascadingconfigError(t *testing.T) {
	if got := cc.CodeOf(nil); got != 0 {
		t.Errorf("CodeOf(nil) = %d, want 0", got)
	}
	if got := cc.CodeOf(errors.New("plain")); got != 0 {
		t.Errorf("CodeOf(plain error) = %d, want 0", got)
	}
}

// api.md Errors, "Several errors in one load": joined errors keep working with errors.Is, errors.As and CodeOf, and
// Errors splits them.
func TestJoinedErrors(t *testing.T) {
	parse := &cc.ParseError{BaseError: base()}
	fetch := &cc.FetchError{BaseError: base()}
	joined := errors.Join(parse, fetch)

	if !errors.Is(joined, cc.ErrParse) || !errors.Is(joined, cc.ErrFetch) {
		t.Errorf("errors.Is on a joined error doesn't find both sentinels")
	}
	if !asType[*cc.ParseError](joined) || !asType[*cc.FetchError](joined) {
		t.Errorf("errors.As on a joined error doesn't find both types")
	}
	if got := cc.CodeOf(joined); got != 400 {
		t.Errorf("CodeOf(joined) = %d, want 400, the first error's code", got)
	}
	if got := cc.Errors(joined); !reflect.DeepEqual(got, []error{parse, fetch}) {
		t.Errorf("Errors(joined) = %v, want [parse fetch]", got)
	}
}

// api.md Errors: Errors splits recursively, and handles single and nil errors.
func TestErrorsSplitting(t *testing.T) {
	a, b, c := errors.New("a"), errors.New("b"), errors.New("c")
	tests := []struct {
		name string
		err  error
		want []error
	}{
		{"nil", nil, nil},
		{"a single error", a, []error{a}},
		{"a join", errors.Join(a, b), []error{a, b}},
		{"a nested join", errors.Join(a, errors.Join(b, c)), []error{a, b, c}},
		{"fmt.Errorf with several %w", fmt.Errorf("%w and %w", a, b), []error{a, b}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := cc.Errors(tc.err); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Errors = %v, want %v", got, tc.want)
			}
		})
	}
}
