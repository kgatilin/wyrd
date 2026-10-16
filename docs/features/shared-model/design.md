# Shared model store

The repo-level daemon (`wyrd serve --multi`) serves one `serve.State` per git
worktree. Each used to parse and hold its own full copy of the package model,
plus one more full copy per materialized review base, and a per-worktree cache
file (`.archai/cache/go-model.json`) with yet another copy on disk. With a
dozen worktrees of one repository nearly every package in every copy is
byte-identical, so the daemon's memory grew linearly with the worktree count.

This design replaces those copies with one content-addressed store per
repository, shared by every State and every review base of the daemon
(`internal/serve/modelstore`). Iteration 1 covers the package model only;
retrieval (BM25, vectors) is iteration 2.

## 1. Three concepts

1. **Version** — one package's `domain.PackageModel`, addressed by the SHA-256
   of its canonical encoding. Held once in memory (the *canonical instance*)
   and written once to disk.
2. **Snapshot** — a commit rev mapped to the (package path, version hash) of
   every package in the tree at that commit. Persisted by rev. Two snapshots of
   `main` a day apart share almost all of their Versions.
3. **Branch view** — a State whose model is a snapshot plus the packages its
   diff against that snapshot's rev touches (committed, uncommitted, untracked —
   the `changedPackages` rule `ReloadWorktree` already used), read from disk on
   top with the snapshot as `known`. The overlay of read packages is the only
   per-worktree model state.

There is no fourth thing. In particular there is no per-worktree cache: a
worktree's warm start *is* a branch view over the snapshot of its own HEAD.

## 2. Canonical encoding and the hash

The encoding is `encoding/json` of the model with `Layer` and `Aggregate`
cleared. JSON writes struct fields in declaration order and the model holds no
maps, so equal models encode to equal bytes, and the bytes hashed are the bytes
stored — there is no second encoder to keep in step. Gob was the alternative:
smaller and faster to decode, but its stream carries type registration state
and is not a canonical form of a value, so it would need its own hash input.

The hash is the same for the same source in two worktrees because no field of
the model carries an absolute path: `SourceFile` and `SymbolRef.File` are base
names, `Span.File` is relative to the module root (`pkg.Module.Dir`),
`Path`/`SymbolRef.Package`/`Imports` are module-relative. A package outside the
main module (no `Module`) would get an absolute `Span.File`, but the reader
skips such packages. The store test parses one source tree in two roots and
compares the hashes.

A package's model is not a function of its own files alone:
`Implementations` and interface-dispatched call edges depend on types declared
elsewhere. The hash is over the model, so this only means a package can have a
new Version because another package changed.

## 3. Load paths

All paths end in `State.packages` holding Versions; every model that enters it
goes through the store, which swaps in the canonical instance when it holds an
identical Version.

- **Cold start, snapshot of HEAD exists** (a restart, any worktree): the
  snapshot plus the packages `git diff HEAD` touches (uncommitted, untracked).
  A clean worktree parses nothing.
- **Cold start on a branch, snapshot of merge-base(base ref, HEAD) exists**:
  the snapshot plus the packages the branch's diff touches. Same code as
  `State.ReloadOver`.
- **Cold start otherwise** — no snapshot for HEAD or the merge base, the diff
  touches `go.mod`/`go.sum`, or the State has no store (tests, `wyrd profile`):
  full parse. If the worktree is clean, and still clean on the same HEAD when
  the parse finishes, the result is recorded as the snapshot of HEAD. This is
  how `main` hands branches their base for free.
- **Refresh** (`ReloadWorktree`): unchanged semantics. On a branch: review base
  + diff packages via `ReloadOver`; on the base worktree, with no base, or a
  diff touching `go.mod`/`go.sum`: full `Reload`, which records a snapshot
  under the same clean-HEAD rule.
- **Watcher edit** (`ReloadPackage`) and `PruneMissingPackages`: change the
  State's map only. Nothing is written to disk any more.
- **Review base** (`ReviewBase`): the two existing fast paths stay (base
  worktree clean on the merge base → its model; this worktree is the merge base
  → nothing to diff). Then the store's snapshot of the merge base. Only when
  none exists is the commit materialized with `git archive`, parsed, recorded,
  and the materialized tree deleted. `basetree.go` is now only the producer of
  snapshots for revs no State has parsed.

Only full parses record snapshots. A branch view is not recorded as the
snapshot of its HEAD even when the worktree is clean: its unchanged packages
come from the base, so their implementations of interfaces the branch added are
missing (the trade-off `ReloadOver` already made), and recording it would
launder that approximation into "the model of this commit" for every later
reader.

## 4. Immutability and where overlay facts live

A Version's model is shared: copying a `PackageModel` by value copies slice
headers, not backing arrays. Rule: **nothing writes through a slice of a model
it did not allocate.** Writing a top-level field of a copy, or reassigning a
top-level slice field of a copy to a new slice, is fine.

- `Layer` and `Aggregate` are per-worktree overlay facts and are never stored:
  a Version always has them zero. They are computed where they always were, by
  `overlay.Merge` on the consumer's own copy of the slice (uigraph, sequence
  API, check, generate). States never set them. Storing them would make the
  same package two Versions in two worktrees whose `wyrd.yaml` differs.
- The canonical instance's top-level slices are clipped to their length, so an
  `append` by any holder reallocates instead of writing into spare capacity
  shared with every other holder (a JSON-decoded slice usually has some).
- `ReviewBase`, `Snapshot` and `CurrentModel` return fresh `[]PackageModel`
  slices, so an in-place `overlay.Merge` on the returned slice cannot reach
  another caller's.

Audit of mutation sites (production code):

- `internal/apply.applyPackage`: a package-level remove/change stored a shallow
  copy of the *current* package in the target, and a later symbol change to the
  same package in the same patch wrote into the current model's arrays
  (in-place delete, element replace, append). Reachable from the MCP
  `apply_diff` tool with a hand-written patch, against the live snapshot. Fixed:
  it uses `clonePackage` like every other target entry.
- `internal/domain/archgraph.normalizePackage` sorts the input model's `Fields`
  and, via `NormalizePackages`, every slice in place. It has no callers; left
  as is and noted here.
- Safe and checked: `diff.Compute` (the normalizers allocate; the past
  `normalizeMethods` bug stays fixed), `overlay.Merge` (top-level scalars on a
  copy), the golang reader's `ReadPartial` (`known` is only read),
  `internal/archreview`, `internal/retrieval`, `internal/adapter/uigraph`,
  `withPrimaryReviewConfig` (copies the overlay config, not models).

## 5. Disk layout and retention

```
<wyrd home>/models/<repo key>/<stamp key>/versions/<hash>.json
<wyrd home>/models/<repo key>/<stamp key>/snapshots/<rev>.json
```

`<wyrd home>` is `WYRD_HOME` or `~/.wyrd` (a `~/.arch` from before the rename is used while it is the only one); `<repo key>` is the registry's key
of the main worktree root, as for the vector cache and the base trees.
`<stamp key>` hashes the reader's version (`golang.ModelVersion`, bumped by
hand when identical source would parse to a different model) with the
structural fingerprint of `domain.PackageModel`: a parser change must not
serve models the old parser produced, and a rebuild that changes neither
keeps them.

Retention:

- On open, every other `<stamp key>` directory is removed — a rebuild discards
  the previous binary's models.
- At most 16 snapshots on disk, least recently used first out (a read touches
  the file's mtime). A Version file is removed when no surviving snapshot and
  no in-memory snapshot references it. Collection runs on open and after each
  recorded snapshot.
- In memory: the same 16 snapshots, LRU. A Version is held while an in-memory
  snapshot references it; States keep their own references alive regardless.
- Materialized base trees are deleted as soon as their snapshot is recorded;
  anything left in the base tree directory (an older daemon, a crash) that is
  not in flight in this process and is older than an hour is swept.

What grows: the versions directory is bounded by the union of 16 snapshots'
Versions — on a slowly moving base, about one tree plus the packages that
changed between those commits.

## 6. What this does not do

- **No symbol-level dedup.** A one-line edit makes a whole new Version of its
  package.
- **Retrieval is untouched.** Each worktree still builds its own BM25 index,
  vector index and `vectors.json`. `Snapshot.Versions` exposes path → hash so
  iteration 2 can key a shared index on Versions.
- **A branch far from any snapshot still parses fully** on cold start, as does
  every worktree on the first start after a rebuild. The branch and `main`
  start concurrently at daemon startup, so on that first start both parse.
- **A Version dropped from memory is not re-deduplicated.** After its last
  in-memory snapshot is evicted, a State that still holds it keeps its
  instance, and an identical Version loaded later is a second allocation.
- **Ignored Go files.** A clean worktree whose tree has gitignored `.go` files
  (generated code) records them in its HEAD snapshot; a snapshot of the same
  rev produced from `git archive` would not have them. Same behaviour as the
  base-worktree fast path before.
- **One daemon per repository.** Two processes sharing a store directory do not
  coordinate: collection in one can remove a Version file the other is about to
  reference (the reader then treats that snapshot as missing and re-parses),
  and a binary with a different reader version removes the other's directory.
