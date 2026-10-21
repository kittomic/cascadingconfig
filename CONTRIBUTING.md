# Contributing

This covers setting up the repository, the checks every change passes, and how changes are made. To report a security
problem, see [SECURITY.md](SECURITY.md) instead of opening an issue.

## Setup

1. Install [mise](https://mise.jdx.dev), then run `mise install` in the repository. It installs the pinned versions of
   Go, golangci-lint, govulncheck, lefthook, gitleaks and rumdl from `.config/mise.toml`.
2. Run `lefthook install` to set up the git hooks.

The repository is a Go workspace (`go.work`) of several modules.
[project-layout.md](docs/contributing/project-layout.md) lists them and what each depends on.

## Checks

Until CI exists, the hooks are the only enforcement, so **never skip them** with `--no-verify` or `LEFTHOOK=0`.

- **Before each commit:** formatting, golangci-lint on what changed, `go mod tidy`, rumdl on staged markdown, and a
  gitleaks scan.
- **Before each push:** every module's tests with `-race` on the latest Go and on the minimum Go, every module on its
  own without `go.work`, golangci-lint and govulncheck everywhere, the licence files, the dependency budget, and rumdl
  on every markdown file, so no link is left broken.
- **Commit messages** follow [Conventional Commits](https://www.conventionalcommits.org), such as `feat(watch): …` or
  `docs: …`.

Each check is a script in `scripts/`, so CI can run the same ones.

## How changes are made

- **Design first.** A behaviour is decided in the docs before code relies on it. When code turns up a question the docs
  don't answer, the work stops until the docs do.
- **Tests first.** Acceptance tests are written from the docs, shown to fail, committed on their own and reviewed before
  the implementation. A test that has been approved changes only in its own `test(scope): revise …` commit, which says
  why.
- **No test reads the real host.** Tests use `cctest`'s in-memory filesystem and fake host. A whole file that
  legitimately trips a linter gets a path rule in `.golangci.yml`, not `//nolint` comments.
- **Branches:** work happens on a branch per milestone, merged into `wip` by a PR with a merge commit, never a squash.
  `main` takes only releases. [The implementation plan](docs/planning/implementation-plan.md#workflow) sets out the
  steps of a milestone.

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
