# Documentation

The docs are kept in four places, by the reader they serve
([decision record 0004](design/decisions/0004-documentation-structure.md)).

## For users

These always describe the library as it is.

- [Precedence](precedence.md): which config files are loaded, and in what order. It's normative: where another doc
  disagrees about order, this one decides.

## Design records

These explain why the library is built as it is. Their code listings record the design when it was decided.

- [Design](design/design.md): how config files are found, merged and trusted, with the decision log.
- [API](design/api.md): the design of the public Go API.
- [Research](design/config-locations.md): how mise and cosmiconfig search for config, which the design builds on.
- [Decision records](design/decisions/): the decisions that weighed alternatives with evidence, are hard to reverse, or
  were contested, one file each.

## Plans

- [Implementation plan](planning/implementation-plan.md): the milestones, and how each one is worked.
- [Future ideas](planning/future-ideas.md): ideas kept for later, none of them decided.

## For contributors

- [Contributing](../CONTRIBUTING.md): setup, checks and how changes are made.
- [Project layout](contributing/project-layout.md): the modules, the tooling and the dependency budget.
