# Parallel dispatch: isolate with worktrees

`task check` (and anything under it: `go vet`, `go build`, the coverage gate) scans by **module path** (`go list -m | sed 's,$,/...,'`), not by which files one ticket touched. Two agents editing disjoint files still share one compile unit: a half-written package from one agent fails `go vet` for the other, even though neither touched the other's files. Same risk for `pnpm`'s TS workspace build.

This is a real failure mode, not a hypothetical: dispatching two parallel code-editing agents against this repo's working tree has produced exactly this — one agent's `task check` failing on a sibling's in-flight untracked package, costing a full rebuild+retest cycle before the sibling finished.

## Fix

Before dispatching genuinely-parallel subagents that will write Go or TS code (not needed for read-only research, or for a single agent, or for edits confined to non-compiled files like docs/config):

```sh
git worktree add ../dexter-<ticket> -b <branch> main
```

One worktree per agent, same object store (cheap — no clone), each agent's `task check` only ever sees its own tree. Merge the branches back (`git merge --no-ff` or cherry-pick) once every agent reports done; conflicts are real signal at that point, not noise from a sibling's unfinished work.

Skip this for a single agent, or when the parallel agents only touch files `task check` never scans (docs, `.github/` comments, etc).
