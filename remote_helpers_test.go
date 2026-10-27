package cascadingconfig_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	cc "github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/cctest"
	ccjson "github.com/kittomic/cascadingconfig/format/json"
)

// The remote include tests load rootCfg, a project file, with remote includes served by fakeResolver, which serves
// files from memory by their normalised reference string, so a test that gets a file back also checks how the loader
// normalised its reference.

var (
	sha40 = strings.Repeat("a1", 20) // a full SHA-1 commit id
	sha64 = strings.Repeat("b2", 32) // a full SHA-256 commit id, or a digest's hex
)

// sum is the hex SHA-256 of s.
func sum(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// fakeResolver is an IncludeResolver for tests. By default it serves files by ref.Ref, and a file it doesn't have is
// a *NotFoundError; fetch, when set, replaces that.
type fakeResolver struct {
	schemes []string
	fetch   func(ctx context.Context, ref cc.IncludeRef) (*cc.Fetched, error)

	mu        sync.Mutex
	files     map[string]cc.Fetched
	errs      map[string]error
	seen      []cc.IncludeRef
	active    int
	maxActive int
}

func newFake(schemes ...string) *fakeResolver {
	return &fakeResolver{schemes: schemes, files: map[string]cc.Fetched{}, errs: map[string]error{}}
}

// serve makes ref (a normalised reference string) return content.
func (f *fakeResolver) serve(ref, content string) *fakeResolver {
	return f.serveFetched(ref, cc.Fetched{Content: []byte(content)})
}

func (f *fakeResolver) serveFetched(ref string, fe cc.Fetched) *fakeResolver {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files[ref] = fe
	delete(f.errs, ref)
	return f
}

// fail makes ref return err.
func (f *fakeResolver) fail(ref string, err error) *fakeResolver {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.errs[ref] = err
	return f
}

func (f *fakeResolver) Schemes() []string { return f.schemes }

func (f *fakeResolver) Resolve(ctx context.Context, ref cc.IncludeRef) (*cc.Fetched, error) {
	f.mu.Lock()
	f.seen = append(f.seen, ref)
	f.active++
	f.maxActive = max(f.maxActive, f.active)
	fetch, err := f.fetch, f.errs[ref.Ref]
	fe, ok := f.files[ref.Ref]
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.active--
		f.mu.Unlock()
	}()
	switch {
	case fetch != nil:
		return fetch(ctx, ref)
	case err != nil:
		return nil, err
	case !ok:
		return nil, &cc.NotFoundError{BaseError: cc.BaseError{Op: "fetch", Path: ref.Ref, Err: errors.New("no such file")}}
	}
	fe.Content = append([]byte(nil), fe.Content...)
	return &fe, nil
}

// refs returns the normalised references the resolver was asked for, in order.
func (f *fakeResolver) refs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, r := range f.seen {
		out = append(out, r.Ref)
	}
	return out
}

// calls returns the references the resolver was asked for, in order.
func (f *fakeResolver) calls() []cc.IncludeRef {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]cc.IncludeRef(nil), f.seen...)
}

// count returns how many times ref was fetched.
func (f *fakeResolver) count(ref string) int {
	n := 0
	for _, r := range f.refs() {
		if r == ref {
			n++
		}
	}
	return n
}

// mediaDec is format/json's decoder as a "toml" format with a media type, so the tests can check format selection
// without the TOML module.
type mediaDec struct{}

func (mediaDec) Format() string                          { return "toml" }
func (mediaDec) Extensions() []string                    { return []string{"toml"} }
func (mediaDec) MediaTypes() []string                    { return []string{"application/toml"} }
func (mediaDec) Decode(data []byte) (cc.Document, error) { return ccjson.Decoder().Decode(data) }

// includeOf is a JSON file holding fields and an include key with refs.
func includeOf(fields string, refs ...string) string {
	b, err := json.Marshal(refs)
	if err != nil {
		panic(err)
	}
	if fields != "" {
		fields += ", "
	}
	return "{" + fields + `"include": ` + string(b) + "}"
}

// remOpts are the options every remote test uses: the fake resolver, no include cache, and "toml" as mediaDec.
func remOpts(f *fakeResolver) cc.Option {
	return cc.Options(cc.WithIncludeResolver(f), cc.WithoutIncludeCache(), cc.WithDecoder(mediaDec{}))
}

// remLoad loads rootCfg including refs, with incScopes, remOpts and opts.
func remLoad(t *testing.T, f *fakeResolver, refs []string, opts ...cc.Option) (*cc.Result, error) {
	t.Helper()
	fsys := cctest.NewFS(map[string]string{rootCfg: includeOf(`"root": 1`, refs...)})
	return incLoad(t, fsys, append([]cc.Option{remOpts(f)}, opts...)...)
}

// remOK is remLoad that fails the test on an error.
func remOK(t *testing.T, f *fakeResolver, refs []string, opts ...cc.Option) *cc.Result {
	t.Helper()
	res, err := remLoad(t, f, refs, opts...)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return res
}

// remFails is remLoad that fails the test if the load succeeds.
func remFails(t *testing.T, f *fakeResolver, refs []string, opts ...cc.Option) (*cc.Result, error) {
	t.Helper()
	res, err := remLoad(t, f, refs, opts...)
	if err == nil {
		t.Fatalf("Load succeeded, want an error")
	}
	if res == nil {
		t.Fatalf("Load returned a nil Result")
	}
	return res, err
}

// anyFile answers every reference with content and media type.
func anyFile(content, mediaType string) func(context.Context, cc.IncludeRef) (*cc.Fetched, error) {
	return func(context.Context, cc.IncludeRef) (*cc.Fetched, error) {
		return &cc.Fetched{Content: []byte(content), MediaType: mediaType}, nil
	}
}

// hasWarning reports whether the report, or any of its layers, has a warning with code.
func hasWarning(rep *cc.Report, code cc.WarningCode) bool {
	if hasWarningCode(rep.Warnings, code) {
		return true
	}
	for _, l := range rep.Layers {
		if hasWarningCode(l.Warnings, code) {
			return true
		}
	}
	return false
}

// timeoutErr is a network-style error that reports a timeout.
type timeoutErr struct{}

func (timeoutErr) Error() string { return "i/o timeout" }
func (timeoutErr) Timeout() bool { return true }
