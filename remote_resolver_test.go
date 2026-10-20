package cascadingconfig_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	cc "github.com/kittomic/cascadingconfig"
)

// DL-196, DL-198, DL-199, DL-205, DL-206: the resolver, size, timeout and cache options reject what can't work.
func TestRemoteOptionErrors(t *testing.T) {
	https := newFake("https")
	runOptionErrorCases(t, []optionErrorCase{
		{name: "nil resolver", opts: []cc.Option{scope, cc.WithIncludeResolver(nil)}, ops: []string{"WithIncludeResolver"}},
		{name: "no schemes", opts: []cc.Option{scope, cc.WithIncludeResolver(newFake())}, ops: []string{"WithIncludeResolver"}},
		{name: "one-letter scheme", opts: []cc.Option{scope, cc.WithIncludeResolver(newFake("c"))}, ops: []string{"WithIncludeResolver"}, contains: []string{`"c"`}},
		{name: "scheme starting with a digit", opts: []cc.Option{scope, cc.WithIncludeResolver(newFake("9p"))}, ops: []string{"WithIncludeResolver"}},
		{name: "scheme with a space", opts: []cc.Option{scope, cc.WithIncludeResolver(newFake("my scheme"))}, ops: []string{"WithIncludeResolver"}},
		{name: "empty scheme", opts: []cc.Option{scope, cc.WithIncludeResolver(newFake("https", ""))}, ops: []string{"WithIncludeResolver"}},
		{name: "file can't be replaced", opts: []cc.Option{scope, cc.WithIncludeResolver(newFake("file"))}, ops: []string{"WithIncludeResolver"}, contains: []string{"file"}},
		{name: "zero timeout", opts: []cc.Option{scope, cc.WithIncludeResolver(https), cc.WithIncludeTimeout(0)}, ops: []string{"WithIncludeTimeout"}},
		{name: "negative timeout", opts: []cc.Option{scope, cc.WithIncludeTimeout(-time.Second)}, ops: []string{"WithIncludeTimeout"}},
		{name: "zero size", opts: []cc.Option{scope, cc.WithMaxIncludeSize(0)}, ops: []string{"WithMaxIncludeSize"}},
		{name: "negative size", opts: []cc.Option{scope, cc.WithMaxIncludeSize(-1)}, ops: []string{"WithMaxIncludeSize"}},
		{name: "relative cache dir", opts: []cc.Option{scope, cc.WithIncludeCache("cache/includes", time.Hour)}, ops: []string{"WithIncludeCache"}},
		{name: "negative TTL", opts: []cc.Option{scope, cc.WithIncludeCache("/tmp/includes", -time.Second)}, ops: []string{"WithIncludeCache"}},
	})
}

// DL-196: a registered resolver handles its schemes, case-insensitively, and the last registration of a scheme wins.
func TestRegisterResolver(t *testing.T) {
	first := newFake("https").serve("https://h.example/a.json", `{"from": "first"}`)
	second := newFake("HTTPS").serve("https://h.example/a.json", `{"from": "second"}`)
	res := remOK(t, first, []string{"https://h.example/a.json"}, cc.WithIncludeResolver(second))
	wantTreeJSON(t, res.Tree(), `{"root": 1, "from": "second"}`)
	if n := len(first.refs()); n != 0 {
		t.Errorf("the replaced resolver was asked %d times, want 0", n)
	}
}

// DL-196: WithoutIncludeResolver removes one scheme, even from a resolver that handles others too.
func TestWithoutOneScheme(t *testing.T) {
	f := newFake("http", "https").serve("http://h.example/a.json", `{"a": 1}`).serve("https://h.example/b.json", `{"b": 1}`)
	res := remOK(t, f, []string{"http://h.example/a.json"}, cc.WithoutIncludeResolver("https"))
	wantTreeJSON(t, res.Tree(), `{"root": 1, "a": 1}`)
	_, err := remFails(t, f, []string{"https://h.example/b.json"}, cc.WithoutIncludeResolver("https"))
	wantErr[*cc.UnsupportedSchemeError](t, err, cc.CodeNotImplemented, rootCfg, "https")
}

// DL-189, DL-196: a policy may name a scheme once a resolver is registered for it.
func TestPolicyNamesRegisteredScheme(t *testing.T) {
	f := newFake("https").serve("https://h.example/a.json", `{"a": 1}`)
	res := remOK(t, f, []string{"https://h.example/a.json"},
		cc.WithDefaultPolicy(cc.AllowResolvers("file", "https"), cc.AllowFor("https", cc.Prefix("https://h.example/"))))
	wantTreeJSON(t, res.Tree(), `{"root": 1, "a": 1}`)
}

// DL-198, design.md Remote references, "Format": the format comes from WithFileFormat rules, then the extension of the
// reference's file path, and only then from the media type the resolver reports; none is a 415 naming the included
// reference. An inner path with no known format is a 415 before anything is fetched.
func TestRemoteFormat(t *testing.T) {
	for _, tc := range []struct {
		name, ref, mediaType string
		opts                 []cc.Option
		format               string // empty: want a 415
	}{
		{name: "extension beats media type", ref: "https://h.example/a.json", mediaType: "application/toml", format: "json"},
		{name: "media type without an extension", ref: "https://h.example/config", mediaType: "application/json; charset=utf-8", format: "json"},
		{name: "another media type", ref: "https://h.example/config", mediaType: "application/toml", format: "toml"},
		{name: "unknown extension falls back", ref: "https://h.example/cfg.php", mediaType: "application/json", format: "json"},
		{name: "query ignored", ref: "https://h.example/a.toml?v=a.json", mediaType: "", format: "toml"},
		{name: "rule on the reference", ref: "https://h.example/raw/cfg", opts: []cc.Option{cc.WithFileFormat(cc.Prefix("https://h.example/raw/"), "toml")}, format: "toml"},
		{name: "unknown media type", ref: "https://h.example/config", mediaType: "text/html"},
		{name: "no media type", ref: "https://h.example/config"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFake("https")
			f.fetch = anyFile(`{"a": 1}`, tc.mediaType)
			res, err := remLoad(t, f, []string{tc.ref}, tc.opts...)
			if tc.format == "" {
				wantErr[*cc.UnsupportedFormatError](t, err, cc.CodeUnsupportedMediaType, tc.ref)
				return
			}
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if l := layerAt(t, res.Describe(), tc.ref); l.Format != tc.format {
				t.Errorf("Format = %q, want %q", l.Format, tc.format)
			}
		})
	}
	t.Run("inner path checked before fetching", func(t *testing.T) {
		f := newFake("git")
		f.fetch = anyFile(`{}`, "application/json")
		ref := "git::https://h.example/r.git//a.conf?ref=v1"
		_, err := remFails(t, f, []string{ref})
		wantErr[*cc.UnsupportedFormatError](t, err, cc.CodeUnsupportedMediaType, ref)
		if n := len(f.refs()); n != 0 {
			t.Errorf("the resolver was asked %d times, want 0", n)
		}
	})
}

// DL-198: WithMaxIncludeSize limits remote content (default 1 MiB), the resolver sees the limit as MaxSize, and content
// over it is a 413, whether the loader or the resolver finds it.
func TestRemoteSizeLimit(t *testing.T) {
	const ref = "https://h.example/a.json"
	content := `{"a": "1234567"}` // 16 bytes
	t.Run("default", func(t *testing.T) {
		f := newFake("https").serve(ref, content)
		remOK(t, f, []string{ref})
		if got := f.calls()[0].MaxSize; got != 1<<20 {
			t.Errorf("MaxSize = %d, want %d", got, 1<<20)
		}
	})
	t.Run("at the limit", func(t *testing.T) {
		f := newFake("https").serve(ref, content)
		remOK(t, f, []string{ref}, cc.WithMaxIncludeSize(16))
		if got := f.calls()[0].MaxSize; got != 16 {
			t.Errorf("MaxSize = %d, want 16", got)
		}
	})
	t.Run("over the limit", func(t *testing.T) {
		f := newFake("https").serve(ref, content)
		_, err := remFails(t, f, []string{ref}, cc.WithMaxIncludeSize(15))
		wantErr[*cc.TooLargeError](t, err, cc.CodeTooLarge, ref)
	})
	t.Run("the resolver's own 413", func(t *testing.T) {
		f := newFake("https").fail(ref, &cc.TooLargeError{BaseError: cc.BaseError{Op: "fetch", Path: ref}})
		_, err := remFails(t, f, []string{ref})
		if code := cc.CodeOf(err); code != cc.CodeTooLarge {
			t.Errorf("code = %d, want 413: %v", code, err)
		}
	})
}

// DL-199: a resolver's error with a cascadingconfig code keeps its type; any other error is a *FetchError naming the
// including file, 504 for a timeout and 502 otherwise.
func TestRemoteErrors(t *testing.T) {
	const ref = "https://h.example/a.json"
	base := cc.BaseError{Op: "fetch", Path: ref, Err: errors.New("cause")}
	for _, tc := range []struct {
		name string
		err  error
		code cc.Code
		own  bool // the loader made a *FetchError, which names the including file
	}{
		{name: "not found", err: &cc.NotFoundError{BaseError: base}, code: cc.CodeNotFound},
		{name: "policy", err: &cc.PolicyError{BaseError: base}, code: cc.CodeForbidden},
		{name: "reference", err: &cc.ReferenceError{BaseError: base}, code: cc.CodeBadRequest},
		{name: "fetch", err: &cc.FetchError{BaseError: base, Timeout: true}, code: cc.CodeGatewayTimeout},
		{name: "plain error", err: errors.New("connection refused"), code: cc.CodeBadGateway, own: true},
		{name: "timeout", err: timeoutErr{}, code: cc.CodeGatewayTimeout, own: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := remFails(t, newFake("https").fail(ref, tc.err), []string{ref})
			if code := cc.CodeOf(err); code != tc.code {
				t.Errorf("code = %d, want %d: %v", code, tc.code, err)
			}
			if !tc.own {
				if !errors.Is(err, tc.err) {
					t.Errorf("error %v doesn't wrap the resolver's error", err)
				}
				return
			}
			wantErr[*cc.FetchError](t, err, tc.code, rootCfg, ref)
			var fe *cc.FetchError
			if errors.As(err, &fe) && (fe.Op != "include" || !errors.Is(err, tc.err)) {
				t.Errorf("Op = %q, want include, wrapping the resolver's error: %v", fe.Op, err)
			}
		})
	}
}

// DL-199: the load's own cancellation stays a 499 cancellation, not a fetch error.
func TestRemoteCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	f := newFake("https")
	f.fetch = func(ctx context.Context, _ cc.IncludeRef) (*cc.Fetched, error) {
		cancel()
		<-ctx.Done()
		return nil, ctx.Err()
	}
	fsys := map[string]string{rootCfg: includeOf("", "https://h.example/a.json")}
	_, err := newLoader(t, fsys, nil, incScopes, remOpts(f)).Load(ctx)
	if !errors.Is(err, context.Canceled) || cc.CodeOf(err) != cc.CodeCanceled {
		t.Errorf("error = %v (code %d), want a 499 that is context.Canceled", err, cc.CodeOf(err))
	}
}

// DL-199: each fetch has a deadline, 30 seconds by default and set with WithIncludeTimeout; running out is a 504.
func TestIncludeTimeout(t *testing.T) {
	const ref = "https://h.example/a.json"
	t.Run("default", func(t *testing.T) {
		var left time.Duration
		f := newFake("https")
		f.fetch = func(ctx context.Context, _ cc.IncludeRef) (*cc.Fetched, error) {
			dl, ok := ctx.Deadline()
			if ok {
				left = time.Until(dl)
			}
			return &cc.Fetched{Content: []byte(`{}`)}, nil
		}
		remOK(t, f, []string{ref})
		if left < 25*time.Second || left > 30*time.Second {
			t.Errorf("time left at the fetch = %v, want about 30s", left)
		}
	})
	t.Run("running out", func(t *testing.T) {
		f := newFake("https")
		f.fetch = func(ctx context.Context, _ cc.IncludeRef) (*cc.Fetched, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		start := time.Now()
		_, err := remFails(t, f, []string{ref}, cc.WithIncludeTimeout(50*time.Millisecond))
		wantErr[*cc.FetchError](t, err, cc.CodeGatewayTimeout, rootCfg, ref)
		if d := time.Since(start); d > 10*time.Second {
			t.Errorf("the load took %v", d)
		}
	})
}

// DL-198: an empty identity is the content's SHA-256; an identity over 256 bytes, or with control characters, is a
// 502, because reports show it.
func TestRemoteIdentity(t *testing.T) {
	const ref = "https://h.example/a.json"
	const content = `{"a": 1}`
	for _, tc := range []struct {
		name, identity, want string
	}{
		{name: "empty", identity: "", want: "sha256:" + sum(content)},
		{name: "given", identity: "commit " + sha40, want: "commit " + sha40},
		{name: "too long", identity: strings.Repeat("x", 257)},
		{name: "control character", identity: "abc\ndef"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFake("https").serveFetched(ref, cc.Fetched{Content: []byte(content), Identity: tc.identity})
			res, err := remLoad(t, f, []string{ref})
			if tc.want == "" {
				wantErr[*cc.FetchError](t, err, cc.CodeBadGateway, rootCfg, ref)
				return
			}
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if l := layerAt(t, res.Describe(), ref); l.Remote == nil || l.Remote.Identity != tc.want {
				t.Errorf("Remote = %+v, want identity %q", l.Remote, tc.want)
			}
		})
	}
}

// DL-198: IncludeRef.Check normalises another reference and checks it under the policy the reference passed: the
// including file's scope's.
func TestIncludeRefCheck(t *testing.T) {
	got := map[string]error{}
	targets := []string{
		"https://h.example/other.json",
		"HTTPS://H.Example/./y.json",
		"https://evil.example/x.json",
		"https://u@h.example/x.json",
		"git::https://h.example/r.git//a.json?ref=v1",
	}
	f := newFake("https", "git")
	f.fetch = func(_ context.Context, ref cc.IncludeRef) (*cc.Fetched, error) {
		for _, tg := range targets {
			got[tg] = ref.Check(tg)
		}
		return &cc.Fetched{Content: []byte(`{}`)}, nil
	}
	remOK(t, f, []string{"https://h.example/a.json"},
		cc.WithScopePolicy("proj", cc.AllowResolvers("file", "https"), cc.AllowFor("https", cc.Prefix("https://h.example/"))))
	if len(got) == 0 {
		t.Fatal("the resolver was never called")
	}
	for _, tg := range targets[:2] {
		if got[tg] != nil {
			t.Errorf("Check(%q) = %v, want nil", tg, got[tg])
		}
	}
	for tg, code := range map[string]cc.Code{targets[2]: cc.CodeForbidden, targets[3]: cc.CodeBadRequest, targets[4]: cc.CodeForbidden} {
		if c := cc.CodeOf(got[tg]); c != code {
			t.Errorf("Check(%q) = %v (code %d), want code %d", tg, got[tg], c, code)
		}
	}
}

// DL-199: references are fetched one at a time.
func TestRemoteOneAtATime(t *testing.T) {
	f := newFake("https")
	f.fetch = func(context.Context, cc.IncludeRef) (*cc.Fetched, error) {
		time.Sleep(5 * time.Millisecond)
		return &cc.Fetched{Content: []byte(`{}`)}, nil
	}
	var refs []string
	for _, n := range []string{"a", "b", "c", "d"} {
		refs = append(refs, "https://h.example/"+n+".json")
	}
	remOK(t, f, refs)
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.seen) != 4 || f.maxActive != 1 {
		t.Errorf("%d fetches, at most %d at once; want 4, one at a time", len(f.seen), f.maxActive)
	}
}
