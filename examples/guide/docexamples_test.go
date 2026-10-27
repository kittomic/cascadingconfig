package guide_test

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"testing"
	"time"

	"github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/cctest"
	"github.com/kittomic/cascadingconfig/httpresolver"
	"github.com/kittomic/cascadingconfig/presets/mise"
	"github.com/kittomic/cascadingconfig/presets/misehardened"
)

// The examples hold every usage snippet marked <!-- example --> in docs/guide (DL-266), each unchanged but for its
// indentation, or a variable it's assigned to, with the setup it needs around it. TestGuideSnippets checks that they
// do. A snippet that names mise.Preset gets a local mise whose Preset is the real one on a cctest world, so no example
// reads the real host. .golangci.yml exempts this file from the linters that the snippets' brevity trips.

// Config is the guide's config type.
type Config struct {
	Server struct {
		Host    string        `koanf:"host"`
		Port    int           `koanf:"port"`
		Timeout time.Duration `koanf:"timeout"`
	} `koanf:"server"`
	Tags []string `koanf:"tags"`
}

// ServerConfig is the server section on its own, for DecodeAt.
type ServerConfig struct {
	Host string `koanf:"host"`
	Port int    `koanf:"port"`
}

const (
	home = "/home/me"
	work = home + "/work"
)

// onWorld is mise.Preset on a cctest world holding files, on a Linux host working in /home/me/work, with env.
func onWorld(files, env map[string]string) struct{ Preset cascadingconfig.Preset } {
	return struct{ Preset cascadingconfig.Preset }{mise.Preset.Extend(
		cascadingconfig.WithFileSystem(cctest.NewFS(files)),
		cctest.WithHost(cctest.Host{GOOS: "linux", GOARCH: "amd64", Home: home, Cwd: work, Env: env}),
	)}
}

// hardenedAt is misehardened.Preset on a cctest world working in cwd, which may be outside the home directory.
func hardenedAt(cwd string, files map[string]string) struct{ Preset cascadingconfig.Preset } {
	return struct{ Preset cascadingconfig.Preset }{misehardened.Preset.Extend(
		cascadingconfig.WithFileSystem(cctest.NewFS(files)),
		cctest.WithHost(cctest.Host{GOOS: "linux", GOARCH: "amd64", Home: home, Cwd: cwd}),
	)}
}

// tree loads preset and returns the merged tree, or prints the error.
func tree(preset cascadingconfig.Preset) cascadingconfig.Tree {
	loader, err := cascadingconfig.NewWithPreset("myapp", preset)
	if err != nil {
		fmt.Println("New:", err)
		return nil
	}
	res, err := loader.Load(context.Background())
	if err != nil {
		fmt.Println("Load:", err)
	}
	return res.Tree()
}

// getting-started.md: a first load.
func Example_firstLoad() {
	ctx := context.Background()
	mise := onWorld(map[string]string{work + "/myapp.toml": "[server]\nport = 8080\n"}, nil)
	err := func() error {
		loader, err := cascadingconfig.NewWithPreset("myapp", mise.Preset)
		if err != nil {
			return err
		}
		cfg, err := cascadingconfig.Load[Config](ctx, loader)
		fmt.Println(cfg.Server.Port)
		return err
	}()
	fmt.Println(err)
	// Output:
	// 8080
	// <nil>
}

// getting-started.md: extending a preset.
func Example_extend() {
	mise := onWorld(map[string]string{
		work + "/myapp.toml":     "[server]\nport = 8080\n",
		work + "/myapp.dev.toml": "[server]\nport = 9000\n",
	}, nil)
	preset := mise.Preset.Extend(cascadingconfig.WithEnvs("dev"))
	loader, _ := cascadingconfig.NewWithPreset("myapp", preset)
	cfg, err := cascadingconfig.Load[Config](context.Background(), loader)
	fmt.Println(cfg.Server.Port, err)
	// Output: 9000 <nil>
}

// getting-started.md: a load's layers.
func Example_layers() {
	ctx := context.Background()
	mise := onWorld(map[string]string{
		"/etc/myapp/config.toml": "[server]\nhost = \"0.0.0.0\"\n",
		work + "/myapp.toml":     "[server]\nport = 8080\n",
		work + "/.myapp.toml":    "",
	}, nil)
	loader, _ := cascadingconfig.NewWithPreset("myapp", mise.Preset)
	err := func() error {
		res, err := loader.Load(ctx)
		if err != nil {
			return err
		}
		for _, layer := range res.Describe().Layers {
			fmt.Println(layer.Scope, layer.Status, layer.Path)
		}
		return nil
	}()
	fmt.Println(err)
	// Output:
	// system loaded /etc/myapp/config.toml
	// project loaded /home/me/work/myapp.toml
	// project empty /home/me/work/.myapp.toml
	// <nil>
}

// sources.md: environments.
func Example_environments() {
	mise := onWorld(map[string]string{
		work + "/myapp.toml":         "[server]\nport = 8080\n",
		work + "/myapp.dev.toml":     "[server]\nport = 9000\nhost = \"dev\"\n",
		work + "/myapp.staging.toml": "[server]\nport = 9100\n",
	}, nil)
	loader, err := cascadingconfig.NewWithPreset("myapp", mise.Preset, cascadingconfig.WithEnvs("dev", "staging"))
	cfg, _ := cascadingconfig.Load[Config](context.Background(), loader)
	fmt.Println(cfg.Server.Host, cfg.Server.Port, err)
	// Output: dev 9100 <nil>
}

// sources.md: environment variables and flags.
func Example_envVarsAndFlags() {
	mise := onWorld(map[string]string{work + "/myapp.toml": "[server]\nport = 8080\nhost = \"file\"\n"},
		map[string]string{"MYAPP_SERVER__PORT": "9090", "MYAPP_SERVER__HOST": "env"})
	flagTree := cascadingconfig.Tree{"server": map[string]any{"host": "flag"}}
	preset := mise.Preset.Extend(
		cascadingconfig.WithScope("env", cascadingconfig.EnvVars("MYAPP_")),
		cascadingconfig.WithScope("flags", cascadingconfig.Values(flagTree)),
	)
	loader, _ := cascadingconfig.NewWithPreset("myapp", preset)
	cfg, err := cascadingconfig.Load[Config](context.Background(), loader)
	fmt.Println(cfg.Server.Host, cfg.Server.Port, err)
	// Output: flag 9090 <nil>
}

// sources.md: explicit config files.
func Example_configFiles() {
	mise := onWorld(map[string]string{
		"/etc/myapp/config.toml": "tags = [\"system\"]\n",
		work + "/myapp.toml":     "[server]\nport = 8080\n",
		"/srv/app.toml":          "[server]\nport = 7000\n",
	}, map[string]string{"MYAPP_SERVER__HOST": "env"})
	configFlag := "/srv/app.toml"
	loader, err := cascadingconfig.NewWithPreset("myapp", mise.Preset, cascadingconfig.WithConfigFiles(configFlag))
	cfg, _ := cascadingconfig.Load[Config](context.Background(), loader)
	fmt.Println(cfg.Server.Port, cfg.Tags, err)
	// Output: 7000 [system] <nil>
}

// sources.md: choosing scopes.
func Example_withoutScopes() {
	mise := onWorld(map[string]string{
		"/etc/myapp/config.toml": "tags = [\"system\"]\n",
		work + "/myapp.toml":     "[server]\nport = 8080\n",
	}, nil)
	preset := mise.Preset.Extend(cascadingconfig.WithoutScopes(cascadingconfig.ScopeSystem))
	loader, _ := cascadingconfig.NewWithPreset("myapp", preset)
	cfg, err := cascadingconfig.Load[Config](context.Background(), loader)
	fmt.Println(cfg.Server.Port, cfg.Tags, err)
	// Output: 8080 [] <nil>
}

// sources.md: a scope of your own.
func Example_mountedScope() {
	// The user directory is outside the project walk here; see sources.md on ~/.config.
	mise := onWorld(map[string]string{
		"/xdg/myapp/config.toml":        "[server]\nport = 8080\nhost = \"user\"\n",
		"/run/config/myapp/config.toml": "[server]\nport = 9999\n",
	}, map[string]string{"XDG_CONFIG_HOME": "/xdg"})
	preset := mise.Preset.Extend(
		cascadingconfig.WithScope("mounted",
			cascadingconfig.FixedDir("/run/config/{name}", cascadingconfig.Pattern{Template: "config.{ext}"}),
			cascadingconfig.After(cascadingconfig.ScopeUser),
		),
	)
	loader, _ := cascadingconfig.NewWithPreset("myapp", preset)
	cfg, err := cascadingconfig.Load[Config](context.Background(), loader)
	fmt.Println(cfg.Server.Host, cfg.Server.Port, err)
	// Output: user 9999 <nil>
}

// decoding.md: the conversions the page names, from an environment variable and a day-suffixed duration.
func Example_conversions() {
	mise := onWorld(map[string]string{work + "/myapp.toml": "[server]\ntimeout = \"7d\"\n"},
		map[string]string{"MYAPP_SERVER__PORT": "8080"})
	loader, _ := cascadingconfig.NewWithPreset("myapp", mise.Preset.Extend(
		cascadingconfig.WithScope("env", cascadingconfig.EnvVars("MYAPP_"))))
	cfg, err := cascadingconfig.Load[Config](context.Background(), loader)
	fmt.Println(cfg.Server.Port, cfg.Server.Timeout, err)
	// Output: 8080 168h0m0s <nil>
}

// decoding.md: one section.
func Example_decodeAt() {
	mise := onWorld(map[string]string{work + "/myapp.toml": "[server]\nport = 8080\nhost = \"h\"\n"}, nil)
	loader, _ := cascadingconfig.NewWithPreset("myapp", mise.Preset)
	res, _ := loader.Load(context.Background())
	err := func() error {
		var server ServerConfig
		if err := cascadingconfig.DecodeAt(res, "server", &server); err != nil {
			return err
		}
		fmt.Println(server.Host, server.Port)
		return nil
	}()
	fmt.Println(err)
	// Output:
	// h 8080
	// <nil>
}

// decoding.md: defaults.
func Example_defaults() {
	mise := onWorld(map[string]string{work + "/myapp.toml": "[server]\nport = 8080\n"}, nil)
	loader, err := cascadingconfig.NewWithPreset("myapp", mise.Preset, cascadingconfig.WithDefaults(Config{
		Tags: []string{"default"},
	}))
	cfg, _ := cascadingconfig.Load[Config](context.Background(), loader)
	fmt.Println(cfg.Server.Port, cfg.Tags, err)
	// Output: 8080 [default] <nil>
}

// decoding.md: unknown keys as errors.
func Example_unknownKeys() {
	ctx := context.Background()
	mise := onWorld(map[string]string{work + "/myapp.toml": "[server]\nprot = 8080\n"}, nil)
	loader, _ := cascadingconfig.NewWithPreset("myapp", mise.Preset,
		cascadingconfig.WithUnknownKeys(cascadingconfig.UnknownKeysError))
	_, err := cascadingconfig.Load[Config](ctx, loader)
	var de *cascadingconfig.DecodeError
	if errors.As(err, &de) && de.Unknown {
		fmt.Printf("%s: %s matches no field\n", de.Path, de.Key)
	}
	// Output: /home/me/work/myapp.toml: server.prot matches no field
}

// decoding.md: requiring config.
func Example_requireConfig() {
	mise := onWorld(map[string]string{"/etc/myapp/config.toml": "tags = [\"system\"]\n"}, nil)
	loader, err := cascadingconfig.NewWithPreset("myapp", mise.Preset,
		cascadingconfig.RequireConfig(cascadingconfig.RequireFile, cascadingconfig.ScopeProject))
	_, loadErr := loader.Load(context.Background())
	var nf *cascadingconfig.NotFoundError
	fmt.Println(err, errors.As(loadErr, &nf))
	// Output: <nil> true
}

// merging.md: appending arrays.
func Example_arrayMerge() {
	mise := onWorld(map[string]string{"/etc/myapp/config.toml": "tags = [\"a\"]\n", work + "/myapp.toml": "tags = [\"b\"]\n"}, nil)
	preset := mise.Preset.Extend(cascadingconfig.WithArrayMerge(cascadingconfig.Append))
	fmt.Println(tree(preset)["tags"])
	// Output: [a b]
}

// merging.md: rules for a key.
func Example_mergeRules() {
	mise := onWorld(map[string]string{
		"/etc/myapp/config.toml": "plugins = [\"x\"]\n[tools.node]\nversion = \"18\"\nflags = [\"--a\"]\n",
		work + "/myapp.toml":     "plugins = [\"y\"]\n[tools.node]\nversion = \"20\"\n",
	}, nil)
	preset := mise.Preset.Extend(
		cascadingconfig.WithMergeRule("tools.*", cascadingconfig.Replace), // a tool's entry is replaced whole
		cascadingconfig.WithMergeRule("plugins", cascadingconfig.Append),  // plugins accumulate across layers
	)
	t := tree(preset)
	fmt.Println(t["tools"], t["plugins"])
	// Output: map[node:map[version:20]] [x y]
}

// merging.md: a merge function.
func Example_mergeFunc() {
	mise := onWorld(map[string]string{
		"/etc/myapp/config.toml": "[limits]\nmax = 100\n",
		work + "/myapp.toml":     "[limits]\nmax = 50\n",
	}, nil)
	preset := mise.Preset.Extend(cascadingconfig.WithMergeFunc("limits.max",
		func(_ cascadingconfig.Path, lower, higher any) (any, error) {
			return max(lower.(int64), higher.(int64)), nil // the largest any layer allows
		}))
	fmt.Println(tree(preset)["limits"])
	// Output: map[max:100]
}

// includes.md: a default policy. The reference isn't allowed, so nothing is fetched.
func Example_includePolicy() {
	mise := onWorld(map[string]string{work + "/myapp.toml": "include = [\"https://elsewhere.example/base.toml\"]\n"}, nil)
	preset := mise.Preset.Extend(cascadingconfig.WithDefaultPolicy(
		cascadingconfig.Allow(cascadingconfig.Prefix("https://config.acme.example/")),
		cascadingconfig.RequirePinned(),
	))
	loader, _ := cascadingconfig.NewWithPreset("myapp", preset)
	_, err := loader.Load(context.Background())
	fmt.Println(errors.Is(err, cascadingconfig.ErrPolicy), cascadingconfig.CodeOf(err) == cascadingconfig.CodeForbidden)
	// Output: true true
}

// includes.md: a scope's policy: project files may include local files only.
func Example_includeScopePolicy() {
	for _, include := range []string{"base.toml", "https://config.acme.example/base.toml"} {
		mise := onWorld(map[string]string{
			work + "/myapp.toml": fmt.Sprintf("include = [%q]\n", include),
			work + "/base.toml":  "[server]\nport = 8080\n",
		}, nil)
		preset := mise.Preset.Extend(cascadingconfig.WithScopePolicy(cascadingconfig.ScopeProject,
			cascadingconfig.InheritDefault(),
			cascadingconfig.AllowResolvers("file"),
		))
		loader, _ := cascadingconfig.NewWithPreset("myapp", preset)
		res, err := loader.Load(context.Background())
		fmt.Println(include, res.Tree()["server"], errors.Is(err, cascadingconfig.ErrPolicy))
	}
	// Output:
	// base.toml map[port:8080] false
	// https://config.acme.example/base.toml <nil> true
}

// includes.md: the cache. No remote include is fetched, so the cache directory is never opened.
func Example_includeCache() {
	mise := onWorld(map[string]string{work + "/myapp.toml": "[server]\nport = 8080\n"}, nil)
	preset := mise.Preset.Extend(cascadingconfig.WithIncludeCache("", 24*time.Hour)) // the default directory
	fmt.Println(tree(preset)["server"])
	// Output: map[port:8080]
}

// security.md: trusting project config. The walk from /srv/checkouts/acme also reaches /srv, which isn't trusted.
func Example_trustPaths() {
	misehardened := hardenedAt("/srv/checkouts/acme", map[string]string{
		"/srv/checkouts/acme/myapp.toml": "[server]\nport = 8080\n",
		"/srv/myapp.toml":                "[server]\nport = 1\n",
	})
	preset := misehardened.Preset.Extend(cascadingconfig.WithScopePolicy(cascadingconfig.ScopeProject,
		cascadingconfig.InheritDefault(), // keep the hardened rules, and add trust
		cascadingconfig.RequireTrust(cascadingconfig.TrustPaths("/srv/checkouts/acme")),
	))
	loader, _ := cascadingconfig.NewWithPreset("myapp", preset, cascadingconfig.WithLogger(slog.New(slog.DiscardHandler)))
	res, err := loader.Load(context.Background())
	for _, l := range res.Describe().Layers {
		fmt.Println(l.Path, l.Status)
	}
	fmt.Println(err)
	// Output:
	// /srv/myapp.toml untrusted
	// /srv/checkouts/acme/myapp.toml loaded
	// <nil>
}

// security.md: remote includes in misehardened.Preset, allowed with an allow-list and pins.
func Example_hardenedRemote() {
	misehardened := hardenedAt(work, nil)
	preset := misehardened.Preset.Extend(
		cascadingconfig.WithIncludeResolver(httpresolver.New()),
		cascadingconfig.WithScopePolicy(cascadingconfig.ScopeSystem,
			cascadingconfig.InheritDefault(),
			cascadingconfig.AllowResolvers("file", "https"),
			cascadingconfig.AllowFor("https", cascadingconfig.Prefix("https://config.acme.example/")),
			cascadingconfig.RequirePinned(), // each reference needs #sha256=…
		),
	)
	_, err := cascadingconfig.NewWithPreset("myapp", preset)
	fmt.Println(err)
	// Output: <nil>
}

// security.md: the permission check in mise.Preset. A group-writable project file is skipped.
func Example_filePermissions() {
	fsys := cctest.NewFS(map[string]string{work + "/myapp.toml": "[server]\nport = 8080\n"})
	fsys.Chmod(work+"/myapp.toml", 0o664)
	mise := struct{ Preset cascadingconfig.Preset }{mise.Preset.Extend(cascadingconfig.WithFileSystem(fsys),
		cctest.WithHost(cctest.Host{GOOS: "linux", GOARCH: "amd64", Home: home, Cwd: work}),
		cascadingconfig.WithLogger(slog.New(slog.DiscardHandler)))}
	preset := mise.Preset.Extend(cascadingconfig.WithScopePolicy(cascadingconfig.ScopeProject,
		cascadingconfig.InheritDefault(),
		cascadingconfig.FilePermissions(cascadingconfig.StrictModes),
	))
	loader, _ := cascadingconfig.NewWithPreset("myapp", preset)
	res, _ := loader.Load(context.Background())
	for _, l := range res.Describe().Layers {
		fmt.Println(l.Status, l.Reason)
	}
	// Output: ignored writable by group
}

// watchWorld is a loader on mise.Preset with a cctest filesystem, host, clock and notifier.
func watchWorld() (*cascadingconfig.Loader, *cctest.FS, *cctest.Notifier, *cctest.Clock) {
	fsys := cctest.NewFS(map[string]string{work + "/myapp.toml": "[server]\nport = 8080\n"})
	notifier, clock := cctest.NewNotifier(), cctest.NewClock(time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC))
	loader, _ := cascadingconfig.NewWithPreset("myapp", mise.Preset, cascadingconfig.WithFileSystem(fsys),
		cctest.WithHost(cctest.Host{GOOS: "linux", GOARCH: "amd64", Home: home, Cwd: work}),
		cctest.WithClock(clock), cascadingconfig.WithNotifier(notifier.New))
	return loader, fsys, notifier, clock
}

// watching.md: watching a typed config.
func Example_watch() {
	ctx := context.Background()
	loader, fsys, notifier, clock := watchWorld()
	err := func() error {
		w, err := cascadingconfig.Watch(ctx, loader, func(ctx context.Context, c cascadingconfig.Change[Config]) {
			if c.Err != nil {
				log.Printf("reload failed, keeping the last good config: %v", c.Err)
				return
			}
			for _, d := range c.Diff {
				log.Printf("config changed: %s", d) // the key and where its value came from, never the value
			}
		})
		if err != nil {
			return err // the first load failed
		}
		defer w.Close()
		cfg := w.Current() // the latest good config, safe from any goroutine
		fmt.Println(cfg.Server.Port)

		fsys.WriteFile(work+"/myapp.toml", []byte("[server]\nport = 9090\n"), 0o644)
		notifier.Send(work + "/myapp.toml")
		clock.Advance(time.Second)
		fmt.Println(w.Current().Server.Port)
		return nil
	}()
	fmt.Println(err)
	// Output:
	// 8080
	// 9090
	// <nil>
}

// watching.md: watch options.
func Example_watchOptions() {
	ctx := context.Background()
	loader, _, _, _ := watchWorld()
	handle := func(context.Context, cascadingconfig.Change[Config]) {}
	w, err := cascadingconfig.Watch(ctx, loader, handle,
		cascadingconfig.WithDebounce(250*time.Millisecond, 2*time.Second),
		cascadingconfig.WithRemotePoll(10*time.Minute), // re-check unpinned remote includes
	)
	fmt.Println(err)
	_ = w.Close()
	// Output: <nil>
}

// watching.md: remote polling rules.
func Example_remotePoll() {
	ctx := context.Background()
	loader, _, _, _ := watchWorld()
	handle := func(context.Context, cascadingconfig.Change[Config]) {}
	w, err := cascadingconfig.Watch(ctx, loader, handle,
		cascadingconfig.WithRemotePoll(time.Hour),
		cascadingconfig.WithRemotePoll(time.Minute, cascadingconfig.Prefix("https://config.acme.example/")),
	)
	fmt.Println(err)
	_ = w.Close()
	// Output: <nil>
}

// provenance-and-logging.md: tracing a key.
func Example_originOf() {
	mise := onWorld(map[string]string{
		"/etc/myapp/config.toml": "[server]\nport = 80\n",
		work + "/myapp.toml":     "[server]\nport = 8080\n",
	}, nil)
	loader, _ := cascadingconfig.NewWithPreset("myapp", mise.Preset)
	res, _ := loader.Load(context.Background())
	origin, ok := cascadingconfig.OriginOf(res, "server.port")
	if ok {
		fmt.Printf("%s by %s (%s)\n", origin.Kind, origin.Layer.Path, origin.Layer.Scope)
		for _, step := range origin.History {
			fmt.Printf("  earlier: %s by %s\n", step.Kind, step.Layer.Path)
		}
	}
	// Output:
	// set by /home/me/work/myapp.toml (project)
	//   earlier: set by /etc/myapp/config.toml
}

// provenance-and-logging.md: logging less.
func Example_logLevel() {
	mise := onWorld(nil, nil)
	loader, err := cascadingconfig.NewWithPreset("myapp", mise.Preset, cascadingconfig.WithLogLevel(slog.LevelWarn))
	fmt.Println(loader != nil, err)
	// Output: true <nil>
}

// testing.md: a fake world, with the real mise.Preset.
func Example_fakeWorld() {
	fsys := cctest.NewFS(map[string]string{
		"/etc/myapp/config.toml":   "[server]\nhost = \"0.0.0.0\"\n",
		"/home/me/work/myapp.toml": "[server]\nport = 8080\n",
	})
	host := cctest.Host{GOOS: "linux", GOARCH: "amd64", Home: "/home/me", Cwd: "/home/me/work",
		Env: map[string]string{"MYAPP_ENV": "dev"}}
	loader, err := cascadingconfig.NewWithPreset("myapp", mise.Preset, cascadingconfig.WithFileSystem(fsys), cctest.WithHost(host))
	res, _ := loader.Load(context.Background())
	fmt.Println(res.Tree()["server"], res.Describe().Envs.Selected, err)
	// Output: map[host:0.0.0.0 port:8080] [dev] <nil>
}

// testing.md: watching with a fake clock and notifier.
func Example_fakeClock() {
	fsys := cctest.NewFS(map[string]string{"/home/me/work/myapp.toml": "[server]\nport = 8080\n"})
	host := cctest.Host{GOOS: "linux", GOARCH: "amd64", Home: "/home/me", Cwd: "/home/me/work"}
	clock := cctest.NewClock(time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC))
	notifier := cctest.NewNotifier()
	loader, err := cascadingconfig.NewWithPreset("myapp", mise.Preset, cascadingconfig.WithFileSystem(fsys),
		cctest.WithHost(host), cctest.WithClock(clock), cascadingconfig.WithNotifier(notifier.New))
	w, _ := cascadingconfig.Watch[Config](context.Background(), loader, nil)
	defer w.Close()
	fsys.WriteFile("/home/me/work/myapp.toml", []byte("[server]\nport = 9090\n"), 0o644)
	notifier.Send("/home/me/work/myapp.toml")
	clock.Advance(time.Second) // past the debounce; returns once the reload and the handler have run
	fmt.Println(w.Current().Server.Port, err)
	// Output: 9090 <nil>
}

// testing.md: golden files.
func TestGoldenReport(t *testing.T) {
	t.Parallel()
	mise := onWorld(map[string]string{work + "/myapp.toml": "[server]\nport = 8080\n"}, nil)
	loader, _ := cascadingconfig.NewWithPreset("myapp", mise.Preset)
	res, err := loader.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	cctest.Golden(t, "report.golden", []byte(res.Describe().String()))
}

// errors.md: handling an error.
func Example_handleError() {
	ctx := context.Background()
	mise := onWorld(map[string]string{work + "/myapp.toml": "[server]\nport = = 1\n"}, nil)
	loader, _ := cascadingconfig.NewWithPreset("myapp", mise.Preset)
	_, err := loader.Load(ctx)
	switch {
	case err == nil:
	case errors.Is(err, cascadingconfig.ErrParse):
		var pe *cascadingconfig.ParseError
		if errors.As(err, &pe) {
			fmt.Printf("%s:%d: fix the syntax\n", pe.Path, pe.Line)
		}
	case cascadingconfig.CodeOf(err) == cascadingconfig.CodeNotFound:
		fmt.Println("no config found")
	default:
		fmt.Println(err)
	}
	// Output: /home/me/work/myapp.toml:2: fix the syntax
}
