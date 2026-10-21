package mise_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing/fstest"
	"time"

	"github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/cctest"
	"github.com/kittomic/cascadingconfig/format/toml"
	"github.com/kittomic/cascadingconfig/httpresolver"
	"github.com/kittomic/cascadingconfig/presets/mise"
	"github.com/kittomic/cascadingconfig/presets/misehardened"
)

// The examples hold every usage snippet marked <!-- example --> in docs/design/design.md and docs/design/api.md
// (DL-250), each unchanged but for its indentation, or a variable it's assigned to, with the setup it needs around it.
// TestDocExamples checks that they do. Every example that loads runs against a cctest.FS on a fake Linux host, never
// the real one. .golangci.yml exempts this file from the linters that the snippets' brevity trips.

// Config is the app's config type the snippets decode into.
type Config struct {
	Port int       `koanf:"port"`
	Log  LogConfig `koanf:"log"`
}

type LogConfig struct {
	Level string `koanf:"level"`
}

var ctx = context.Background()

// world is a cctest.FS holding files, on a Linux host with home /home/me, working in /home/me/work, with env.
func world(files map[string]string, env map[string]string) cascadingconfig.Option {
	return cascadingconfig.Options(
		cascadingconfig.WithFileSystem(cctest.NewFS(files)),
		cctest.WithHost(cctest.Host{GOOS: "linux", GOARCH: "amd64", Home: "/home/me", Cwd: "/home/me/work", Env: env}),
	)
}

// load loads preset in world and returns its tree, or prints the error.
func load(preset cascadingconfig.Preset, opts ...cascadingconfig.Option) cascadingconfig.Tree {
	l, err := cascadingconfig.NewWithPreset("myapp", preset, opts...)
	if err != nil {
		fmt.Println("New:", err)
		return nil
	}
	res, err := l.Load(ctx)
	if err != nil {
		fmt.Println("Load:", err)
		return nil
	}
	return res.Tree()
}

// design.md: Choosing scopes.
func Example_choosingScopes() {
	// a system daemon: only /etc/{name} (or %ProgramData%\{name})
	daemon := misehardened.Preset.Extend(
		cascadingconfig.WithScopes(cascadingconfig.ScopeSystem),
	)

	// everything misehardened.Preset has, minus system config
	noSystem := misehardened.Preset.Extend(
		cascadingconfig.WithoutScopes(cascadingconfig.ScopeSystem),
	)

	for _, p := range []cascadingconfig.Preset{daemon, noSystem} {
		l, err := cascadingconfig.NewWithPreset("myapp", p, world(nil, nil))
		if err != nil {
			fmt.Println(err)
			continue
		}
		res, _ := l.Load(ctx)
		var selected []string
		for _, s := range res.Describe().Scopes {
			if s.Selected && s.Name != cascadingconfig.ScopeDefaults {
				selected = append(selected, string(s.Name))
			}
		}
		fmt.Println(selected)
	}
	// Output:
	// [system]
	// [user project override]
}

// design.md: Custom scopes.
func Example_customScopes() {
	defaultsFS := fstest.MapFS{"myapp.toml": {Data: []byte("port = 1\nlog.level = \"debug\"\n")}}
	preset := mise.Preset.Extend(
		// Built-in defaults compiled into the binary, below everything else.
		cascadingconfig.WithScope("builtin",
			cascadingconfig.EmbeddedFS(defaultsFS, cascadingconfig.Pattern{Template: "{name}.toml"}),
			cascadingconfig.Before(cascadingconfig.ScopeSystem),
			cascadingconfig.Trust(cascadingconfig.TrustSystem),
		),
		// Kubernetes-mounted config, above user config.
		cascadingconfig.WithScope("mounted",
			cascadingconfig.FixedDir("/run/secrets/{name}", cascadingconfig.Pattern{Template: "config.{ext}"}),
			cascadingconfig.After(cascadingconfig.ScopeUser),
		),
		cascadingconfig.WithScopes(
			"builtin", cascadingconfig.ScopeSystem, cascadingconfig.ScopeUser, "mounted",
			cascadingconfig.ScopeProject, cascadingconfig.ScopeOverride,
		),
	)
	tree := load(preset, world(map[string]string{"/run/secrets/myapp/config.toml": "port = 2\n"}, nil))
	fmt.Println(tree["port"], tree["log"])
	// Output: 2 map[level:debug]
}

// design.md: Per-key merge rules, mise's section semantics.
func Example_mergeRules() {
	tools := cascadingconfig.WithMergeRule("tools.*", cascadingconfig.Replace) // nearest tool entry replaces it whole
	tasks := cascadingconfig.WithMergeRule("tasks.*", cascadingconfig.Replace) // nearest task replaces it whole
	daemons := cascadingconfig.WithMergeRule("daemons.*", cascadingconfig.Replace)
	// [env], [vars], [settings]: per-key, which DeepMerge already does

	tree := load(mise.Preset.Extend(tools, tasks, daemons), world(map[string]string{
		"/home/me/myapp.toml":      "[tools.node]\nversion = \"18\"\ncorepack = true\n[env]\nA = \"1\"\n",
		"/home/me/work/myapp.toml": "[tools.node]\nversion = \"20\"\n[env]\nB = \"2\"\n",
	}, nil))
	fmt.Println(tree["tools"], tree["env"])
	// Output: map[node:map[version:20]] map[A:1 B:2]
}

// design.md: Choosing a file's format.
func Example_fileFormats() {
	rc := cascadingconfig.WithFileFormat(cascadingconfig.FileName(".{name}rc"), "yaml", "json") // YAML if registered, else JSON
	conf := cascadingconfig.WithFileFormat(cascadingconfig.Extension(".conf", ".cfg"), "toml")
	settings := cascadingconfig.WithFileFormat(cascadingconfig.Glob("/etc/**/*.settings"), "yaml")
	re := cascadingconfig.WithFileFormat(cascadingconfig.Regexp(`.*/\.?myapprc$`), "yaml")

	preset := mise.Preset.Extend(rc, conf, settings, re,
		cascadingconfig.WithScope("extra", cascadingconfig.FixedDir("/srv",
			cascadingconfig.Pattern{Template: ".{name}rc"}, cascadingconfig.Pattern{Template: "app.conf"})),
		cascadingconfig.WithScopes("extra"))
	tree := load(preset, world(map[string]string{"/srv/.myapprc": "a: yaml\n", "/srv/app.conf": "b = \"toml\"\n"}, nil))
	fmt.Println(tree["a"], tree["b"])
	// Output: yaml toml
}

// design.md: Allow-lists.
func Example_allowLists() {
	policy := cascadingconfig.WithDefaultPolicy(
		cascadingconfig.Allow( // every scheme
			cascadingconfig.Prefix("https://config.example.com/"),
		),
		cascadingconfig.AllowFor("git",
			cascadingconfig.Glob("git::https://github.com/acme/*//**"),
			cascadingconfig.MatchRef(func(ref cascadingconfig.IncludeRef) bool {
				return ref.From.Scope != cascadingconfig.ScopeProject // only user/system files may fetch
			}),
		),
	)
	l, err := cascadingconfig.NewWithPreset("myapp", mise.Preset, policy, world(nil, nil))
	if err != nil {
		fmt.Println(err)
		return
	}
	res, _ := l.Load(ctx)
	for _, s := range res.Describe().Scopes {
		if s.Name == cascadingconfig.ScopeProject {
			fmt.Println(s.Policy.Allow, s.Policy.AllowFor["git"])
		}
	}
	// Output: [Prefix("https://config.example.com/")] [Glob("git::https://github.com/acme/*//**") MatchRef]
}

// design.md: misehardened.Preset, adding a remote resolver back.
func Example_hardenedRemotes() {
	preset := misehardened.Preset.Extend(
		cascadingconfig.WithIncludeResolver(httpresolver.New()),
		// Only system config may fetch, and only from the internal config host.
		cascadingconfig.WithScopePolicy(cascadingconfig.ScopeSystem,
			cascadingconfig.InheritDefault(),
			cascadingconfig.AllowResolvers("file", "https"),
			cascadingconfig.AllowFor("https", cascadingconfig.Prefix("https://config.acme.internal/")),
			cascadingconfig.RequirePinned(), // required in misehardened: refs need #sha256=…
		),
		// Project config may only include files inside its own project.
		cascadingconfig.WithScopePolicy(cascadingconfig.ScopeProject,
			cascadingconfig.InheritDefault(),
			cascadingconfig.Confine("{config_root}"),
		),
	)
	l, err := cascadingconfig.NewWithPreset("myapp", preset, world(nil, nil))
	if err != nil {
		fmt.Println(err)
		return
	}
	res, _ := l.Load(ctx)
	for _, s := range res.Describe().Scopes {
		if s.Name == cascadingconfig.ScopeSystem {
			fmt.Println(s.Policy.Resolvers, s.Policy.RequirePinned)
		}
	}
	// Output: [file https] true
}

// approved is a TrustStore that trusts the paths it holds.
type approved map[string]bool

func (a approved) Trusted(_ context.Context, path, _ string) (bool, error) { return a[path], nil }

// design.md: Trust store.
func Example_trustStore() {
	store := approved{"/home/me/work/myapp.toml": true}
	trust := cascadingconfig.WithScopePolicy(cascadingconfig.ScopeProject,
		cascadingconfig.InheritDefault(),
		cascadingconfig.RequireTrust(store),
	)
	files := map[string]string{"/home/me/myapp.toml": "port = 1\n", "/home/me/work/myapp.toml": "port = 2\n"}
	l, err := cascadingconfig.NewWithPreset("myapp", mise.Preset, trust, world(files, nil))
	if err != nil {
		fmt.Println(err)
		return
	}
	res, _ := l.Load(ctx)
	for _, layer := range res.Describe().Layers {
		fmt.Println(layer.Path, layer.Status)
	}
	// Output:
	// /home/me/myapp.toml untrusted
	// /home/me/work/myapp.toml loaded
}

// api.md: Errors, a coded error built in another module.
func Example_fetchError() {
	ref, cause := "https://config.example.com/team.toml", errors.New("i/o timeout")
	err := &cascadingconfig.FetchError{
		BaseError: cascadingconfig.BaseError{Op: "fetch", Path: ref, Err: cause},
		Timeout:   true, // Code() is then CodeGatewayTimeout; otherwise CodeBadGateway
	}
	fmt.Println(cascadingconfig.CodeOf(err) == cascadingconfig.CodeGatewayTimeout)
	// Output: true
}

// design.md: Explicit config files.
func Example_configFiles() {
	flagValues := []string{"/srv/app.toml"}
	files := cascadingconfig.WithConfigFiles(flagValues...) // usually fed from a --config flag
	tree := load(mise.Preset, files, world(map[string]string{"/srv/app.toml": "port = 9\n"}, nil))
	fmt.Println(tree["port"])
	// Output: 9
}

// design.md: Value overrides.
func Example_valueOverrides() {
	flagTree := cascadingconfig.Tree{"server": map[string]any{"host": "flag.example"}}
	preset := mise.Preset.Extend(
		cascadingconfig.WithScope("env",
			cascadingconfig.EnvVars("MYAPP_"), // MYAPP_SERVER__PORT=8080 → server.port = "8080"
			cascadingconfig.After(cascadingconfig.ScopeOverride),
		),
		cascadingconfig.WithScope("flags",
			cascadingconfig.Values(flagTree), // the app's parsed flags, as a tree
			cascadingconfig.After("env"),
		),
		cascadingconfig.WithScopes(cascadingconfig.ScopeSystem, cascadingconfig.ScopeUser, cascadingconfig.ScopeProject,
			cascadingconfig.ScopeOverride, "env", "flags"),
	)
	tree := load(preset, world(nil, map[string]string{"MYAPP_SERVER__PORT": "8080"}))
	fmt.Println(tree["server"])
	// Output: map[host:flag.example port:8080]
}

// expandVars replaces ${ROOT} in string values with root, as an app's own expansion might.
func expandVars(tree cascadingconfig.Tree, root string) (cascadingconfig.Tree, error) {
	for k, v := range tree {
		if s, ok := v.(string); ok {
			tree[k] = strings.ReplaceAll(s, "${ROOT}", root)
		}
	}
	return tree, nil
}

// validate requires a port.
func validate(tree cascadingconfig.Tree) error {
	if _, ok := tree["port"]; !ok {
		return errors.New("no port")
	}
	return nil
}

// design.md: Transform hooks.
func Example_transforms() {
	layer := cascadingconfig.WithLayerTransform(func(
		ctx context.Context, l cascadingconfig.Layer, tree cascadingconfig.Tree,
	) (cascadingconfig.Tree, error) {
		return expandVars(tree, l.ConfigRoot) // the app's own expansion, e.g. os.Expand
	})
	result := cascadingconfig.WithResultTransform(func(ctx context.Context, tree cascadingconfig.Tree) (cascadingconfig.Tree, error) {
		return tree, validate(tree)
	})
	tree := load(mise.Preset.Extend(layer, result), world(map[string]string{
		"/home/me/work/myapp.toml": "port = 1\ndata = \"${ROOT}/data\"\n",
	}, nil))
	fmt.Println(tree["data"])
	// Output: /home/me/work/data
}

// design.md: Hot reload.
func Example_watch() {
	n := cctest.NewNotifier()
	loader, err := cascadingconfig.NewWithPreset("myapp", mise.Preset, cascadingconfig.WithNotifier(n.New),
		world(map[string]string{"/home/me/work/myapp.toml": "port = 7\n"}, nil))
	if err != nil {
		fmt.Println(err)
		return
	}
	w, err := cascadingconfig.Watch[Config](ctx, loader, func(c cascadingconfig.Change[Config]) {
		// c.Old, c.New: decoded snapshots; c.Diff: changed keys with provenance; c.Err: reload failure
	}, cascadingconfig.WithDebounce(100*time.Millisecond, time.Second))
	defer w.Close()
	cfg := w.Current() // lock-free read of the latest good snapshot
	_ = err
	fmt.Println(cfg.Port)
	// Output: 7
}

// api.md: Package layout, batteries included. The local mise shadows the package, so the snippet's mise.Preset runs
// on a cctest.FS.
func Example_batteriesIncluded() {
	mise := struct{ Preset cascadingconfig.Preset }{mise.Preset.Extend(world(nil, nil))}

	// Batteries included: everything mise does.
	loader, err := cascadingconfig.NewWithPreset("myapp", mise.Preset)

	fmt.Println(loader != nil, err)
	// Output: true <nil>
}

// api.md: Package layout, minimal. It builds a loader on the real host, but never loads, so it reads no files there.
func Example_minimal() {
	// Minimal: mise's layout, TOML only. Core layouts register no remotes and no fsnotify.
	// Module graph: cascadingconfig, mapstructure, cascadingconfig/format/toml, go-toml.
	loader, err := cascadingconfig.NewWithPreset("myapp", cascadingconfig.MiseLayout.Extend(
		cascadingconfig.WithDecoder(toml.Decoder()),
	))

	fmt.Println(loader != nil, err)
	// Output: true <nil>
}

// api.md: Defaults.
func Example_defaults() {
	defaults := cascadingconfig.WithDefaults(Config{
		Port: 8080,
		Log:  LogConfig{Level: "info"},
	})
	loader, err := cascadingconfig.NewWithPreset("myapp", mise.Preset, defaults, world(nil, nil))
	if err != nil {
		fmt.Println(err)
		return
	}
	cfg, err := cascadingconfig.Load[Config](ctx, loader)
	fmt.Println(cfg.Port, cfg.Log.Level, err)
	// Output: 8080 info <nil>
}

// api.md: Requiring config.
func Example_requireConfig() {
	require := cascadingconfig.Options(
		cascadingconfig.RequireConfig(cascadingconfig.RequireAny), // something, anywhere
		cascadingconfig.RequireConfig(cascadingconfig.RequireFile|cascadingconfig.RequireNonEmpty, // and a real system file
			cascadingconfig.ScopeSystem),
	)
	for _, files := range []map[string]string{{"/etc/myapp/config.toml": "a = 1\n"}, nil} {
		l, err := cascadingconfig.NewWithPreset("myapp", mise.Preset, require, world(files, nil))
		if err != nil {
			fmt.Println(err)
			return
		}
		_, err = l.Load(ctx)
		fmt.Println(cascadingconfig.CodeOf(err))
	}
	// Output:
	// 0
	// 404
}

// api.md: Overview. The local mise shadows the package, so the snippet's mise.Preset, which it uses with no options,
// runs on a cctest.FS with a fake notifier.
func Example_overview() {
	mise := struct{ Preset cascadingconfig.Preset }{mise.Preset.Extend(
		world(map[string]string{"/home/me/work/myapp.toml": "port = 5\n"}, nil),
		cascadingconfig.WithNotifier(cctest.NewNotifier().New),
	)}

	loader, err := cascadingconfig.NewWithPreset("myapp", mise.Preset /*, opts... */)

	// typed: the main use case
	cfg, err := cascadingconfig.Load[Config](ctx, loader)

	// untyped, or both
	res, err := loader.Load(ctx)
	tree := res.Tree()     // merged tree: map[string]any
	err = res.Decode(&cfg) // decode it whenever you like
	report := res.Describe()

	// optional: watch for changes
	w, err := cascadingconfig.Watch[Config](ctx, loader, func(c cascadingconfig.Change[Config]) { /* ... */ })
	defer w.Close()

	_, _, _ = err, tree, report
	fmt.Println(cfg.Port)
	// Output: 5
}
