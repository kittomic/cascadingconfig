package guide_test

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/cctest"
	"github.com/kittomic/cascadingconfig/presets/mise"
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
