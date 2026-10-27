# Contributing

Thanks for helping. This covers what to do before you start, setting up the repository, the checks every change passes,
and how a change is made. To report a security problem, see [SECURITY.md](SECURITY.md) instead of opening an issue.

## Before you start

- **Features and behaviour changes:** open an issue first, so the design can be agreed before you write code. The
  library promises exact precedence and security behaviour, so most changes need a decision before an implementation.
- **Bug fixes and docs:** a PR is fine on its own. A bug fix comes with a test that fails without it.

## Setup

1. Install [mise](https://mise.jdx.dev), then run `mise install` in the repository. It installs the pinned versions of
   Go, golangci-lint, govulncheck, lefthook, gitleaks and rumdl from `.config/mise.toml`.
2. Run `lefthook install` to set up the git hooks.

The repository is a Go workspace (`go.work`) of several modules.
[project-layout.md](docs/contributing/project-layout.md) lists them and what each depends on.

## Checks

CI runs the same checks on every PR, on Linux, with the tests also on macOS and Windows. The hooks catch problems before
they reach CI, so **never skip them** with `--no-verify` or `LEFTHOOK=0`.

- **Before each commit:** formatting, golangci-lint on what changed, `go mod tidy`, rumdl on staged markdown, and a
  gitleaks scan.
- **Before each push:** every module's tests with `-race` on the latest Go and on the minimum Go, every module on its
  own without `go.work`, golangci-lint and govulncheck everywhere, the licence files, the dependency budget, and rumdl
  on every markdown file, so no link is left broken.
- **Commit messages** follow [Conventional Commits](https://www.conventionalcommits.org), such as `feat(watch): …` or
  `docs: …`.

Each check is a script in `scripts/`, and `.github/workflows/ci.yml` runs the same ones.

## How a change is made

- **Docs first.** A behaviour is decided in the docs before code relies on it: the [guide](docs/guide/) for what users
  see, and design.md's [decision log](docs/design/design.md#decision-log) for why. If your change raises a question the
  docs don't answer, ask it in the issue or PR.
- **Tests first.** A new behaviour comes with tests written from the docs, which fail without the change. Changing an
  existing test's expectations needs its own commit, `test(scope): revise …`, which says why.
- **No test reads the real host.** Tests use `cctest`'s in-memory filesystem and fake host, never your home directory or
  environment. A whole file that legitimately trips a linter gets a path rule in `.golangci.yml`, not `//nolint`
  comments.
- **No new dependencies** without an issue first: each module's dependencies are budgeted in
  [project-layout.md](docs/contributing/project-layout.md#dependency-analysis).

## Pull requests

- **Target `main`,** the only long-lived branch. Releases are tags on it.
- **Merges are merge commits,** never squashes, so keep each commit one logical change with its own message.
- **The PR template** lists what a reviewer checks.
- **Licensing:** your contribution is under the [Apache License 2.0](LICENSE), as its section 5 provides. There is no
  CLA, and commits need no sign-off.

The maintainer's own workflow, a milestone at a time, is in
[the implementation plan](docs/planning/implementation-plan.md#workflow).

## Decision records

Every decision gets a row in design.md's [decision log](docs/design/design.md#decision-log), cited in code and tests as
`DL-n`. A decision also gets a [decision record](docs/design/decisions/0000-template.md) when any of these is true
(DL-255):

- alternatives were weighed with evidence, such as probes or fuzzing
- it's hard to reverse: the public API's shape, the module layout or the docs' structure
- its recommendation was contested

The row then links to the record.

## Where the docs live

- **`docs/`:** user docs, which always describe the library as it is.
- **`docs/design/`:** design records, which explain why it's built as it is.
- **`docs/planning/`:** plans for what comes next.
- **`docs/contributing/`:** how the repository is laid out and built.

[docs/README.md](docs/README.md) lists every doc.
