# Staged verification

`npm ci` installs a Husky pre-commit hook; run `npm run prepare` to enable it in an
existing checkout. `npm run verify:commit` runs the same checks without committing.
The Go, Buf, browser, and container prerequisites are the same as for workspace
`verify`. Husky uses ordinary Git hooks; CI enforces `ocm-fleet:check` separately.

The hook checks a private linked worktree containing the **Git index**, including
partially staged files. It starts with `ocm-fleet:lint`, tries `ocm-fleet:fmt` if
needed, then runs `ocm-fleet:check` against the exact proposed commit.

- Fixes can be published only to originally staged, regular files whose working
  contents and permissions still match the staged version. Safe fixes update both
  the working file and index, after checks pass.
- If any fix touches unstaged work or a file outside that set, the entire proposal
  is discarded. Checks run on the original staged contents and report issues.
  Partially staged files can pass checks, but the hook does not merge fixes into
  their unstaged edits.
- Failed checks leave source and staging unchanged. Existing unstaged and
  untracked files stay in place throughout; there is no stash to restore.
- If a check fails, run `npx nx run ocm-fleet:verify`, review its changes, and stage
  the intended changes before retrying. Nx prints paths to detailed failure logs.

The adapter reuses installed external npm dependencies and redirects workspace
package links into the snapshot. It refuses mismatched staged dependencies;
run `npm ci` against the intended manifests and lockfile before retrying. Builds
and generated outputs belong to the private checkout. This is isolation from
ordinary unstaged source, not a security sandbox for running untrusted tasks.

Nx's default task cache and database are shared between linked worktrees. Running
`verify` before committing warms the expensive checks when their staged inputs
match. A warm read-only lint pass skips formatting entirely. Snapshot creation,
graph setup and deliberately uncached policy checks still run; differences between
staged and unstaged inputs correctly cause cache misses. Custom cache-directory
overrides or restricted access to the shared cache can prevent this reuse.

The root Dockerfile uses the explicit Podman `image:*` targets. It is excluded
from Docker inference because the plugin's default image tag embeds the absolute
checkout path, invalidating unrelated workspace task caches across checkouts.
The Go toolchain fingerprint also suppresses Node warnings: nested Nx color
settings can otherwise emit a warning with a process ID, which changes the hash.
Build, lint, and test commands keep their normal diagnostics.

The adapter takes Git's index lock and rechecks affected files before publishing.
Avoid editing those files during publication: multiple working files and the index
cannot be updated in a single filesystem transaction. Ordinary write failures
attempt rollback. A crash or forced termination during publication may require
recovery copies in the temporary `ocm-fleet-staged-*` directory (`original-index`
and `files.json`, with original and formatted file bytes encoded as base64).

To skip hooks for one commit, use `HUSKY=0 git commit ...` or `git commit --no-verify`.
CI installs with `HUSKY=0` and runs the same read-only quality targets directly.
