# 0003: The shape of a watch Diff

**Date:** 2026-10-21. **Status:** decided; see the outcome below and DL-234 in
[design.md](../design.md#decision-log).

This weighs what `Change.Diff` lists when tables appear, empty, fill or turn into scalars. DL-221 says "leaves only; an
array is one leaf; a table that becomes a scalar, or the reverse, is one `DiffChanged`". It doesn't say what happens to
an empty table `{}`, which has no leaves. The test review raised it, and the M9 tests don't settle it yet.

**Sources:** a probe run on 2026-10-21: six candidate rules, written as small reference models, each checked against the
properties below on 2 million random pairs of trees. Half of the pairs are one small edit apart, and half are unrelated.
Trees have keys `a` and `b`, up to three levels deep, with integers, one-element arrays, empty tables and non-empty
tables as values.

---

## The properties

A good `Diff` would have all of these:

- **P1, empty exactly when equal:** an empty `Diff` means nothing changed. Otherwise the handler is called with a change
  it can't see.
- **P2, replayable:** applying the `Diff` to `Old` gives `New`. The replay rule is part of each candidate. The most
  forgiving one, which every candidate gets: removals first, then additions and changes, in path order, with a removal
  also removing tables it leaves empty.
- **P3, no nesting:** no entry's path is inside another entry's path.
- **P4, symmetric:** the diff from `New` to `Old` is the same diff with added and removed swapped.
- **P6, distinguishing:** two different `New` trees never give the same `Diff` from the same `Old`.
- **P7, every key listed:** every scalar or array whose value changed, appeared or disappeared has an entry at its own
  path. Then "did `server.port` change?" is one exact lookup in `Diff`, with no prefix matching.

## The candidates

- **A, leaves only, `{}` ignored:** the model the M9 tests use now.
- **B, `{}` is a leaf:** two tables always compare key by key.
- **B2, `{}` is a leaf, and a table that becomes `{}`, or the reverse, is one change** at the table's path.
- **L, the difference of the leaf sets, `{}` a leaf:** keeps DL-221's rule that a table becoming a scalar is one change.
- **L′, the pure difference of the leaf sets, `{}` a leaf:** no exceptions. A path that's a leaf on both sides and
  differs is `changed`; only in `Old`, `removed`; only in `New`, `added`.
- **C, whole subtrees:** an added or removed table is one entry, with the table as its value; two tables compare key by
  key.

## Results

Each cell is "holds", or how often the property failed in 2 million pairs.

| Rule | P1 empty ⇔ equal | P2 replayable | P3 no nesting | P4 symmetric | P6 distinguishing | P7 every key listed | Entries per pair |
| ---- | ---------------- | ------------- | ------------- | ------------ | ----------------- | ------------------- | ---------------: |
| A    | 22%              | 36%           | holds         | holds        | 16%               | 5.4%                |             0.76 |
| B    | holds            | 3.2%          | holds         | holds        | 1.1%              | 5.4%                |             1.14 |
| B2   | holds            | holds         | holds         | holds        | holds             | 11%                 |             1.12 |
| L    | holds            | holds         | 7.9%          | holds        | holds             | 5.4%                |             1.22 |
| L′   | holds            | holds         | 16%           | holds        | holds             | **holds**           |             1.33 |
| C    | holds            | holds¹        | holds         | holds        | holds             | 20%                 |             1.07 |

¹ With a plain replay, without removing emptied tables; with the forgiving replay it fails in 3.2%.

**The smallest failures:**

- **A, P1:** `{}` → `{"b": {}}` gives an empty `Diff`, so the handler is called with nothing in it.
- **B, P6:** `{"b": {"b": 0}}` → `{"b": {}}` and `{"b": {"b": 0}}` → `{}` both give `[removed b.b]`, so the app can't
  tell whether `b` survived.
- **B2, P7:** `{"b": {"b": 0}}` → `{"b": {}}` gives `[changed b {"b":0} → {}]`. There's no entry for `b.b`, so an app
  watching `b.b` misses its removal.
- **DL-221's exception, P7** (A, B, L): `{"b": {"b": 1}}` → `{"b": 0}` gives `[changed b {"b":1} → 0]`, with no entry
  for `b.b`.
- **C, P7:** `{}` → `{"b": {"b": 1}}` gives `[added b → {"b":1}]`, with no entry for `b.b`.
- **L′, P3:** `{"b": {"b": 1}}` → `{"b": 0}` gives `[added b → 0, removed b.b 1 →]`: `b.b` sits inside `b`.

## No rule can have everything

P3, P6 and P7 can't all hold. Take `Old = {"b": {"b": 0}}` and the two edits `New₁ = {"b": {}}` and `New₂ = {}`:

1. P7 makes both diffs contain `removed b.b`.
2. P6 makes them differ, so one of them needs another entry.
3. The only other path in play is `b`, and an entry there sits outside `b.b`'s, which P3 forbids.

So each rule gives one up: B gives up P6, B2 and C give up P7, and L and L′ give up P3.

## Options

1. **L′, the pure leaf-set difference (recommended).**
   - **Definition, in one sentence:** `Diff` compares the leaves of the two trees, where a leaf is a scalar, an array or
     an empty table.
   - **Gains:** the only rule where every changed key has its own entry (P7), so a handler checks a key by exact path.
     No exceptions to explain.
   - **Costs:** when a value changes shape, its entries nest. `{"b": {"b": 1}}` → `{"b": 0}` gives `added b` and
     `removed b.b`, sorted by path, so `b` comes first. A replay must apply removals first. About 20% more entries than
     B2.
   - **Changes:** DL-221's "a table that becomes a scalar is one `DiffChanged`" goes. The approved `TestWatchDiff`
     expects `changed tbl` for a table that became `"flat"`; it would expect `removed tbl.x`, `removed tbl.y` and
     `added tbl` instead. That's a `test: revise` commit.
2. **B2, `{}` a leaf, emptying or filling a table is one change.**
   - **Gains:** no nesting, and DL-221 stays as written, with one added sentence. No approved test changes.
   - **Costs:** a key inside a table that empties or turns into a scalar has no entry. An app watching it has to check
     every prefix of its path too.
3. **C, whole subtrees.**
   - **Gains:** the fewest entries, and the plainest replay.
   - **Costs:** P7 fails most often (20%): any key inside an added or removed table needs a prefix check. DL-221's
     "leaves only" goes, and `TestWatchDiff`'s `added new.n` would become `added new`.

A and B lose information outright, so they're not options.

## What the tests would check

Whichever rule is chosen, `FuzzDiff` gets a generator that includes empty tables, uses the chosen rule as its reference
model, and also checks the properties the rule keeps, so they're enforced on the implementation and not only on a model.

## Outcome

**Option 1, L′, the pure leaf-set difference**, decided on 2026-10-21 and recorded as DL-234:

- **`Diff` compares the leaves of the two trees**, where a leaf is a scalar, an array or an empty table. A path that's a
  leaf on both sides with different values is `DiffChanged`, one only in `Old` is `DiffRemoved`, and one only in `New`
  is `DiffAdded`. Entries are sorted by path, segment by segment, so `b` comes before `b.b`.
- **DL-221's exception goes:** a table that becomes a scalar is its leaves removed and the scalar added.
- **A replay** applies removals first, then additions and changes, and a removal also removes tables it leaves empty.
- **Tests:** `TestWatchDiff` is revised to the new shape, and `FuzzDiff` generates empty tables, uses L′ as its
  reference model, and checks that the `Diff` replays `Old` into `New` and that the reverse reload gives the reverse
  `Diff`.
