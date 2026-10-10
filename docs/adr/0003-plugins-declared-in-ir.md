# Declared plugins are part of the IR, not just a local cache

Phase 2 originally deferred plugin-install reproducibility: `dex plugin add` would write only the gitignored `.dex/plugins/` cache, so a fresh clone had to remember which `dex plugin add` commands to rerun by hand. Reversed in grilling (Q8/Q17): `defineDexConfig({ project, plugins: [...] })` now declares the install set as source, and `DexIR` gains a sibling `plugins: string[]` field next to `project`/`resources` — the first change to that top-level shape since Phase 1.

AGENTS.md calls the IR one of the two contracts that "changing either is one change across every side": SDK emitter, engine parser, adapters, SPEC. This earns that weight: once real `dex.config.ts` files declare `plugins`, removing the field is a breaking change, not a local cleanup. `dex plugin add <repo>[@tag]` still exists standalone for ad-hoc installs that never get committed; `dex plugin install` is new, reads only the declared list.
