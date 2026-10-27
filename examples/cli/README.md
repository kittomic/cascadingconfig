# cli: flags beat files, and users can ask why

A command-line tool reads a config file but lets every setting be overridden by a flag. When a user is surprised by a
value, they need to find out where it came from. `ccx-cli` uses `cosmiconfig.Preset`: the first config file found wins,
`-config` names a file explicitly, the flags given on the command line go on top, and `-explain` traces any setting.

## Run it

```console
$ go run .
Hello, world!
$ go run . -name Ada -explain name
name = Ada
  set by the flags scope
  earlier: set by …/examples/cli/.ccx-clirc (project)
```

## Things to try

- **A flag beats the file:** `go run . -name Ada` prints `Hello, Ada!`. The greeting still comes from `.ccx-clirc`,
  because only the flags given become a layer.
- **Name a file:** `go run . -config team.yaml` prints `HOWDY, TEAM!`. An explicit file replaces the files the tool
  would have found, as cosmiconfig does, but flags still beat it: `go run . -config team.yaml -shout=false`.
- **Explain a file's setting:** `go run . -explain greeting` shows the file that set it.
- **Move the config:** cosmiconfig's other places work too, such as `.config/ccx-clirc.yaml` or `.ccx-clirc.json`. If
  more than one exists, the first in cosmiconfig's order wins.

## What it shows

- `cosmiconfig.Preset`, with a `Values` scope above the files for the flags that were set (`flag.Visit`).
- `WithConfigFiles` for a `-config` flag.
- `OriginOf` and `DecodeAt` to explain one key: the layer that set it, and what earlier layers did to it.
