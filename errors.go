package cascadingconfig

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Code classifies an error. Codes follow HTTP status codes where that makes sense.
type Code int

// Codes returned by Code methods and CodeOf.
const (
	CodeBadRequest           Code = 400
	CodeForbidden            Code = 403
	CodeNotFound             Code = 404
	CodeConflict             Code = 409
	CodeTooLarge             Code = 413
	CodeUnsupportedMediaType Code = 415
	CodeUnprocessable        Code = 422
	CodeCanceled             Code = 499 // nginx's "client closed request"
	CodeInternal             Code = 500
	CodeNotImplemented       Code = 501
	CodeBadGateway           Code = 502
	CodeGatewayTimeout       Code = 504
	CodeLoopDetected         Code = 508
)

// Coder is implemented by every cascadingconfig error.
type Coder interface {
	error
	Code() Code
}

// BaseError is the base type that every specific error embeds. Its fields are exported, so other modules, such as
// resolvers and decoders, can build cascadingconfig errors themselves.
//
// It isn't called Error: a struct can't have a field and a method with the same name, so an embedded field named
// Error would hide the Error method.
type BaseError struct {
	Op    string // the operation, such as "load", "decode", "include" or "reload"
	Path  string // the file or reference involved, if any
	Scope string // the scope involved, if any
	Err   error  // the underlying cause

	code Code // set only by NewError; specific types define their own Code
}

// NewError builds a base error for situations with no specific type, such as cancellation (CodeCanceled) or a failed
// app callback (CodeInternal).
func NewError(code Code, op, path string, err error) *BaseError {
	return &BaseError{Op: op, Path: path, Err: err, code: code}
}

// Code returns the code given to NewError.
func (e *BaseError) Code() Code {
	return e.code
}

// Unwrap returns the underlying cause.
func (e *BaseError) Unwrap() error {
	return e.Err
}

// Error returns "cascadingconfig: <op> <path>: <cause>", or "cascadingconfig: <op>: <cause>" without a path.
func (e *BaseError) Error() string {
	return e.format(e.Path)
}

// format builds the message with location in place of the path, so specific types can add a position.
func (e *BaseError) format(location string) string {
	var b strings.Builder
	b.WriteString("cascadingconfig:")
	if e.Op != "" {
		b.WriteString(" ")
		b.WriteString(e.Op)
	}
	if location != "" {
		b.WriteString(" ")
		b.WriteString(location)
	}
	if e.Err != nil {
		b.WriteString(": ")
		b.WriteString(e.Err.Error())
	}
	return b.String()
}

// CodeOf returns the code of the first cascadingconfig error in err's chain, or 0.
func CodeOf(err error) Code {
	var c Coder
	if errors.As(err, &c) {
		return c.Code()
	}
	return 0
}

// Errors splits err into its individual errors. Any error with an Unwrap() []error method, such as one from
// errors.Join, is split, recursively. A single error is returned on its own, and nil gives nil.
func Errors(err error) []error {
	if err == nil {
		return nil
	}
	multi, ok := err.(interface{ Unwrap() []error })
	if !ok {
		return []error{err}
	}
	var out []error
	for _, e := range multi.Unwrap() {
		out = append(out, Errors(e)...)
	}
	return out
}

// OptionError reports invalid loader configuration, such as an unknown scope, an invalid environment name, a bad
// formats list or ambiguous merge rules. New returns it for options; Load returns it for values read from environment
// variables or bootstrap files.
type OptionError struct {
	BaseError
}

// Code returns CodeBadRequest.
func (e *OptionError) Code() Code {
	return CodeBadRequest
}

// Is reports whether target is ErrInvalidOption.
func (e *OptionError) Is(target error) bool {
	return target == ErrInvalidOption
}

// ErrInvalidOption is the sentinel for *OptionError.
var ErrInvalidOption = errors.New("cascadingconfig: invalid option")

// ParseError reports a file that isn't valid in its format, breaks a format dialect rule, or has a malformed reserved
// key.
type ParseError struct {
	BaseError

	Format       string // the decoder's format, such as "json"
	Line, Column int    // 1-based position of the problem; 0 when the decoder doesn't know
}

// Code returns CodeBadRequest.
func (e *ParseError) Code() Code {
	return CodeBadRequest
}

// Is reports whether target is ErrParse.
func (e *ParseError) Is(target error) bool {
	return target == ErrParse
}

// ErrParse is the sentinel for *ParseError.
var ErrParse = errors.New("cascadingconfig: parse error")

// Error returns "cascadingconfig: <op> <path>:<line>:<col>: <cause>" when the position is known, and the BaseError form
// otherwise.
func (e *ParseError) Error() string {
	if e.Line <= 0 {
		return e.BaseError.Error()
	}
	location := e.Path + ":" + strconv.Itoa(e.Line)
	if e.Column > 0 {
		location += ":" + strconv.Itoa(e.Column)
	}
	return e.format(location)
}

// ReferenceError reports an include reference that fails validation, including traversal attempts.
type ReferenceError struct {
	BaseError
}

// Code returns CodeBadRequest.
func (e *ReferenceError) Code() Code {
	return CodeBadRequest
}

// Is reports whether target is ErrInvalidReference.
func (e *ReferenceError) Is(target error) bool {
	return target == ErrInvalidReference
}

// ErrInvalidReference is the sentinel for *ReferenceError.
var ErrInvalidReference = errors.New("cascadingconfig: invalid reference")

// PolicyError reports something blocked by policy: an allow-list, a scheme the policy doesn't allow, the trust store, a
// permission policy in error mode, confinement, or an unpinned remote.
type PolicyError struct {
	BaseError
}

// Code returns CodeForbidden.
func (e *PolicyError) Code() Code {
	return CodeForbidden
}

// Is reports whether target is ErrPolicy.
func (e *PolicyError) Is(target error) bool {
	return target == ErrPolicy
}

// ErrPolicy is the sentinel for *PolicyError.
var ErrPolicy = errors.New("cascadingconfig: blocked by policy")

// NotFoundError reports an explicit config file or include target that doesn't exist, or an unmet RequireConfig.
type NotFoundError struct {
	BaseError
}

// Code returns CodeNotFound.
func (e *NotFoundError) Code() Code {
	return CodeNotFound
}

// Is reports whether target is ErrNotFound.
func (e *NotFoundError) Is(target error) bool {
	return target == ErrNotFound
}

// ErrNotFound is the sentinel for *NotFoundError.
var ErrNotFound = errors.New("cascadingconfig: not found")

// ConflictError reports more than one file in one slot under ConflictFail. Path is the highest-ranked file.
type ConflictError struct {
	BaseError

	Files []string // every file in the slot, highest-ranked first
}

// Code returns CodeConflict.
func (e *ConflictError) Code() Code {
	return CodeConflict
}

// Is reports whether target is ErrConflict.
func (e *ConflictError) Is(target error) bool {
	return target == ErrConflict
}

// ErrConflict is the sentinel for *ConflictError.
var ErrConflict = errors.New("cascadingconfig: conflict")

// TooLargeError reports a file or remote include over its size limit.
type TooLargeError struct {
	BaseError
}

// Code returns CodeTooLarge.
func (e *TooLargeError) Code() Code {
	return CodeTooLarge
}

// Is reports whether target is ErrTooLarge.
func (e *TooLargeError) Is(target error) bool {
	return target == ErrTooLarge
}

// ErrTooLarge is the sentinel for *TooLargeError.
var ErrTooLarge = errors.New("cascadingconfig: too large")

// UnsupportedFormatError reports a format, extension or media type with no registered decoder.
type UnsupportedFormatError struct {
	BaseError
}

// Code returns CodeUnsupportedMediaType.
func (e *UnsupportedFormatError) Code() Code {
	return CodeUnsupportedMediaType
}

// Is reports whether target is ErrUnsupportedFormat.
func (e *UnsupportedFormatError) Is(target error) bool {
	return target == ErrUnsupportedFormat
}

// ErrUnsupportedFormat is the sentinel for *UnsupportedFormatError.
var ErrUnsupportedFormat = errors.New("cascadingconfig: unsupported format")

// DecodeError reports a failure to decode one key of the merged tree: a type mismatch, or an unknown key in strict
// mode. Path is the file that set the key. The message never quotes the value; the underlying error may.
type DecodeError struct {
	BaseError

	Key     Path // the key that failed
	Unknown bool // the key matches no field, under UnknownKeysError

	detail string // what went wrong, without the value
	source string // the file, variable or scope that set the key
}

// Error returns "cascadingconfig: decode: <key>: <what went wrong>, set by <source>", which never quotes the value. An
// error built elsewhere, without those details, is formatted as a BaseError.
func (e *DecodeError) Error() string {
	if e.detail == "" {
		return e.BaseError.Error()
	}
	msg := fmt.Sprintf("cascadingconfig: %s: %s: %s", e.Op, e.Key, e.detail)
	if e.source != "" {
		msg += ", set by " + e.source
	}
	return msg
}

// Code returns CodeUnprocessable.
func (e *DecodeError) Code() Code {
	return CodeUnprocessable
}

// Is reports whether target is ErrDecode.
func (e *DecodeError) Is(target error) bool {
	return target == ErrDecode
}

// ErrDecode is the sentinel for *DecodeError.
var ErrDecode = errors.New("cascadingconfig: decode error")

// MergeError reports a merge rule that met the wrong type, or a MergeFunc that returned an error. Path is the higher
// layer's file, and the message names both files.
type MergeError struct {
	BaseError

	Key   Path   // where in the tree
	Lower string // the lower layer's file
}

// Code returns CodeUnprocessable.
func (e *MergeError) Code() Code {
	return CodeUnprocessable
}

// Is reports whether target is ErrMerge.
func (e *MergeError) Is(target error) bool {
	return target == ErrMerge
}

// ErrMerge is the sentinel for *MergeError.
var ErrMerge = errors.New("cascadingconfig: merge error")

// TransformError reports a transform hook that returned an error.
type TransformError struct {
	BaseError
}

// Code returns CodeUnprocessable.
func (e *TransformError) Code() Code {
	return CodeUnprocessable
}

// Is reports whether target is ErrTransform.
func (e *TransformError) Is(target error) bool {
	return target == ErrTransform
}

// ErrTransform is the sentinel for *TransformError.
var ErrTransform = errors.New("cascadingconfig: transform error")

// IOError reports a read that failed for a reason other than "not found", such as permission denied, or a directory
// where a file was expected.
type IOError struct {
	BaseError
}

// Code returns CodeInternal.
func (e *IOError) Code() Code {
	return CodeInternal
}

// Is reports whether target is ErrIO.
func (e *IOError) Is(target error) bool {
	return target == ErrIO
}

// ErrIO is the sentinel for *IOError.
var ErrIO = errors.New("cascadingconfig: I/O error")

// UnsupportedSchemeError reports an include whose scheme has no registered resolver.
type UnsupportedSchemeError struct {
	BaseError
}

// Code returns CodeNotImplemented.
func (e *UnsupportedSchemeError) Code() Code {
	return CodeNotImplemented
}

// Is reports whether target is ErrUnsupportedScheme.
func (e *UnsupportedSchemeError) Is(target error) bool {
	return target == ErrUnsupportedScheme
}

// ErrUnsupportedScheme is the sentinel for *UnsupportedSchemeError.
var ErrUnsupportedScheme = errors.New("cascadingconfig: unsupported scheme")

// FetchError reports a remote include that couldn't be fetched. Its code is CodeBadGateway, or CodeGatewayTimeout when
// Timeout is set.
type FetchError struct {
	BaseError

	Timeout bool // the fetch timed out
}

// Code returns CodeBadGateway, or CodeGatewayTimeout when Timeout is set.
func (e *FetchError) Code() Code {
	if e.Timeout {
		return CodeGatewayTimeout
	}
	return CodeBadGateway
}

// Is reports whether target is ErrFetch.
func (e *FetchError) Is(target error) bool {
	return target == ErrFetch
}

// ErrFetch is the sentinel for *FetchError.
var ErrFetch = errors.New("cascadingconfig: fetch failed")

// CycleError reports an include cycle, in presets that allow nested includes.
type CycleError struct {
	BaseError
}

// Code returns CodeLoopDetected.
func (e *CycleError) Code() Code {
	return CodeLoopDetected
}

// Is reports whether target is ErrIncludeCycle.
func (e *CycleError) Is(target error) bool {
	return target == ErrIncludeCycle
}

// ErrIncludeCycle is the sentinel for *CycleError.
var ErrIncludeCycle = errors.New("cascadingconfig: include cycle")
