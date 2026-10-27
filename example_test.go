// The examples use Unix paths in cctest's world, so, like the tests that do, they don't run on Windows.

//go:build !windows

package cascadingconfig_test

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/cctest"
)

// Config is the examples' app config.
type Config struct {
	Server struct {
		Host string `koanf:"host"`
		Port int    `koanf:"port"`
	} `koanf:"server"`
}

// A loader built from a layout reads system, user and project config, and decodes the merged result into a struct.
// The examples run in a world of their own, from cctest, so they read nothing from the machine they run on.
func Example() {
	fsys := cctest.NewFS(map[string]string{
		"/etc/myapp/config.json":   `{"server": {"host": "0.0.0.0", "port": 80}}`,
		"/home/me/work/myapp.json": `{"server": {"port": 8080}}`,
	})
	host := cctest.Host{GOOS: "linux", GOARCH: "amd64", Home: "/home/me", Cwd: "/home/me/work"}

	loader, err := cascadingconfig.NewWithPreset("myapp", cascadingconfig.MiseLayout,
		cascadingconfig.WithFileSystem(fsys), cctest.WithHost(host))
	if err != nil {
		log.Fatal(err)
	}
	cfg, err := cascadingconfig.Load[Config](context.Background(), loader)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(cfg.Server.Host, cfg.Server.Port)
	// Output: 0.0.0.0 8080
}

// An EnvVars scope reads environment variables with a prefix, and a double underscore separates the key's segments.
// Added with WithScope, it ranks above every file.
func ExampleEnvVars() {
	fsys := cctest.NewFS(map[string]string{"/home/me/work/myapp.json": `{"server": {"port": 8080}}`})
	host := cctest.Host{GOOS: "linux", GOARCH: "amd64", Home: "/home/me", Cwd: "/home/me/work",
		Env: map[string]string{"MYAPP_SERVER__PORT": "9090"}}

	loader, err := cascadingconfig.NewWithPreset("myapp", cascadingconfig.MiseLayout,
		cascadingconfig.WithFileSystem(fsys), cctest.WithHost(host),
		cascadingconfig.WithScope("env", cascadingconfig.EnvVars("MYAPP_")))
	if err != nil {
		log.Fatal(err)
	}
	cfg, err := cascadingconfig.Load[Config](context.Background(), loader)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(cfg.Server.Port)
	// Output: 9090
}

// OriginOf says which layer set a key, and what it overrode.
func ExampleOriginOf() {
	fsys := cctest.NewFS(map[string]string{
		"/etc/myapp/config.json":   `{"server": {"port": 80}}`,
		"/home/me/work/myapp.json": `{"server": {"port": 8080}}`,
	})
	host := cctest.Host{GOOS: "linux", GOARCH: "amd64", Home: "/home/me", Cwd: "/home/me/work"}
	loader, err := cascadingconfig.NewWithPreset("myapp", cascadingconfig.MiseLayout,
		cascadingconfig.WithFileSystem(fsys), cctest.WithHost(host))
	if err != nil {
		log.Fatal(err)
	}
	res, err := loader.Load(context.Background())
	if err != nil {
		log.Fatal(err)
	}

	origin, ok := cascadingconfig.OriginOf(res, "server.port")
	if !ok {
		log.Fatal("server.port isn't set")
	}
	fmt.Println(origin.Kind, origin.Layer.Scope, origin.Layer.Path)
	for _, step := range origin.History {
		fmt.Println("  over", step.Kind, step.Layer.Scope, step.Layer.Path)
	}
	// Output:
	// set project /home/me/work/myapp.json
	//   over set system /etc/myapp/config.json
}

// Describe reports what a load found: each layer, with its scope, its status and its path.
func ExampleResult_Describe() {
	fsys := cctest.NewFS(map[string]string{
		"/etc/myapp/config.json":   `{"server": {"port": 80}}`,
		"/home/me/work/myapp.json": ``,
	})
	host := cctest.Host{GOOS: "linux", GOARCH: "amd64", Home: "/home/me", Cwd: "/home/me/work"}
	loader, err := cascadingconfig.NewWithPreset("myapp", cascadingconfig.MiseLayout,
		cascadingconfig.WithFileSystem(fsys), cctest.WithHost(host))
	if err != nil {
		log.Fatal(err)
	}
	res, err := loader.Load(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	for _, layer := range res.Describe().Layers {
		fmt.Println(layer.Scope, layer.Status, layer.Path)
	}
	// Output:
	// system loaded /etc/myapp/config.json
	// project empty /home/me/work/myapp.json
}

// A merge rule changes how one key merges. Here a project's plugins add to the system's, instead of replacing them.
func ExampleWithMergeRule() {
	fsys := cctest.NewFS(map[string]string{
		"/etc/myapp/config.json":   `{"plugins": ["audit"]}`,
		"/home/me/work/myapp.json": `{"plugins": ["lint"]}`,
	})
	host := cctest.Host{GOOS: "linux", GOARCH: "amd64", Home: "/home/me", Cwd: "/home/me/work"}
	loader, err := cascadingconfig.NewWithPreset("myapp", cascadingconfig.MiseLayout,
		cascadingconfig.WithFileSystem(fsys), cctest.WithHost(host),
		cascadingconfig.WithMergeRule("plugins", cascadingconfig.Append))
	if err != nil {
		log.Fatal(err)
	}
	res, err := loader.Load(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(res.Tree()["plugins"])
	// Output: [audit lint]
}

// A failed load returns every problem its stage found, joined. Errors splits them, and CodeOf gives each one's code, an
// HTTP status.
func ExampleErrors() {
	fsys := cctest.NewFS(map[string]string{
		"/etc/myapp/config.json":   `{"server": {"port": 80,}}`,
		"/home/me/work/myapp.json": `{"server": {"port": 8080`,
	})
	host := cctest.Host{GOOS: "linux", GOARCH: "amd64", Home: "/home/me", Cwd: "/home/me/work"}
	loader, err := cascadingconfig.NewWithPreset("myapp", cascadingconfig.MiseLayout,
		cascadingconfig.WithFileSystem(fsys), cctest.WithHost(host))
	if err != nil {
		log.Fatal(err)
	}
	_, err = loader.Load(context.Background())
	for _, e := range cascadingconfig.Errors(err) {
		var pe *cascadingconfig.ParseError
		if errors.As(e, &pe) {
			fmt.Printf("%d %s:%d:%d\n", cascadingconfig.CodeOf(e), pe.Path, pe.Line, pe.Column)
		}
	}
	// Output:
	// 400 /etc/myapp/config.json:1:24
	// 400 /home/me/work/myapp.json:1:25
}

// A watcher reloads when a file changes, and calls the handler with the new config and what changed. The fake clock
// and notifier from cctest let the example run without waiting.
func ExampleWatch() {
	fsys := cctest.NewFS(map[string]string{"/home/me/work/myapp.json": `{"server": {"port": 8080}}`})
	host := cctest.Host{GOOS: "linux", GOARCH: "amd64", Home: "/home/me", Cwd: "/home/me/work"}
	clock := cctest.NewClock(time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC))
	notifier := cctest.NewNotifier()
	loader, err := cascadingconfig.NewWithPreset("myapp", cascadingconfig.MiseLayout,
		cascadingconfig.WithFileSystem(fsys), cctest.WithHost(host), cctest.WithClock(clock),
		cascadingconfig.WithNotifier(notifier.New))
	if err != nil {
		log.Fatal(err)
	}

	w, err := cascadingconfig.Watch(context.Background(), loader, func(_ context.Context, c cascadingconfig.Change[Config]) {
		if c.Err != nil {
			fmt.Println("reload failed:", c.Err)
			return
		}
		fmt.Println("port", c.Old.Server.Port, "->", c.New.Server.Port)
		for _, d := range c.Diff {
			fmt.Println(d.Kind, d.Path, d.Old, d.New)
		}
	})
	if err != nil {
		log.Fatal(err)
	}
	defer w.Close()

	// An edit, as an app's user would make it, and the event a real notifier would send.
	fsys.WriteFile("/home/me/work/myapp.json", []byte(`{"server": {"port": 9090}}`), 0o644)
	notifier.Send("/home/me/work/myapp.json")
	clock.Advance(time.Second) // past the debounce; returns once the handler has run
	// Output:
	// port 8080 -> 9090
	// changed server.port 8080 9090
}
