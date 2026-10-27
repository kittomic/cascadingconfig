# 0004: Documentation structure

**Date:** 2026-10-22. **Status:** decided; see the outcome below and DL-254 and DL-255 in
[design.md](../design.md#decision-log).

This decides where each doc lives and what it promises its reader. Until M10, every doc sat in `docs/` and served as
plan, design and reference at once. Before the README and user guide are written (M11d), user docs need separating from
planning, and the API audit (M11b) needs to know which docs the code is checked against.

**Sources:** the M11 and M11a gap lists of 2026-10-21 and 2026-10-22, and a probe of rumdl 0.3.0's link rules.

---

## The question

Which docs are for users, which explain the design, which plan the work, and which tell contributors how to work? Each
kind ages differently:

- **User docs** must always describe the library as it is.
- **Design records** explain why the library is built as it is. They keep growing, since every milestone adds rows to
  the decision log, but their listings record the design when it was decided, not the current API.
- **Plans** say what comes next, and go stale once it's done.
- **Contributor docs** say how to work on the repository, and change with the tooling.

## Options

1. **Three homes: user, contributor and planning, with the design records under planning.** Proposed first, in the M11
   gap list. It files design.md, api.md and the decision records as plans, though they explain the code and keep
   growing.
2. **Four homes, adding design (recommended).** Each kind gets its own directory.
3. **Leave everything in `docs/`, with a header on each doc.** No links break, but a reader can't tell the kinds apart
   from the tree, and the user guide would sit among 6,000 lines of design.

## Checking links

Moving files breaks links. The probe found that rumdl, which pre-commit already runs, reports links to missing files
(MD057), missing headings in the same file and missing headings in another file (MD051). Pre-commit checks only staged
files, though, so renaming a heading in one doc can break a link in another without notice. Running rumdl on every
markdown file in pre-push closes that gap without a new script. The repository passed a full run before the move.

## Outcome

**Option 2, four homes**, decided on 2026-10-22 and recorded as DL-254:

- **User docs, `docs/`:** precedence.md, which is normative and which users need, and the guides added in M11d.
- **Design records, `docs/design/`:** design.md, api.md, config-locations.md (the research behind the design) and the
  decision records in `decisions/`.
- **Plans, `docs/planning/`:** implementation-plan.md and future-ideas.md.
- **Contributor docs, `docs/contributing/`:** project-layout.md, and later the release procedure. `CONTRIBUTING.md` in
  the repository root is the entry point.
- **Names don't change,** so the code comments that cite design.md or api.md by name stay right. Files move with
  `git mv`, so their history follows them.
- **Each doc says which kind it is** in its opening lines, and `docs/README.md` indexes them.
- **Pre-push runs `rumdl check .`** on every markdown file.

**When a decision gets a record**, recorded as DL-255: every decision gets a row in the decision log. It also gets a
record like this one when alternatives were weighed with evidence, when it's hard to reverse (the public API's shape,
the module layout, the docs' structure), or when its recommendation was contested. The row then links to the record.
Existing rows aren't backfilled. Records follow [the template](0000-template.md).
