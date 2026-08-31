# Vendored: impeccable

`skill/` and `agents/` are a snapshot of the **impeccable** design skill by
Philipp Bakaus, redistributed here so `arnes init impeccable` works offline.

- Upstream: https://github.com/pbakaus/impeccable
- Pinned commit: `b0594c72d18006b5865c70eb3a97e8b04064e600`
- License: Apache License 2.0 (see the upstream `LICENSE` / `NOTICE.md`)

`impeccable-shim.mjs` is **not** part of impeccable — it is arnes code that
adapts the arnes hook contract to impeccable's Claude Code hook scripts.

To refresh the snapshot: re-vendor from a newer upstream commit, update the
pin above, and re-test the shim against it.
