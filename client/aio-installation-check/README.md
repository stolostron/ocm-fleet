# AIO installation check

Standalone Ink preflight utility with an OME / FleetShift banner, bordered readiness panels, and inline keyboard menus. The specification in [`docs/specs/aio-installation-check.md`](../../docs/specs/aio-installation-check.md) records Perry 0.5.1520 as the validated PoC compiler. ScriptC was evaluated and removed from the current PoC build; rationale appears in the [implementation decisions](../../docs/specs/aio-installation-check-decisions.md).

For rationale behind the native build, compiler choice, shared Ink presentation layer, Rust toolchain, and compatibility shims, see the [implementation decisions](../../docs/specs/aio-installation-check-decisions.md).

## Terminal interaction

- Use **↑ / ↓** to move focus (wrapping at either end), then **Enter** to confirm. The preferred runtime receives initial focus.
- **Ctrl-C** cancels with exit code 130 and restores raw mode and cursor visibility. Menus clean up their input, resize, and signal listeners before returning.
- Panels and confirmed selections stay in normal scrollback. Only the small active menu is redrawn; no alternate screen or terminal-history clearing is used.
- Frames use the current terminal width without an arbitrary cap, reserving the last physical column to prevent autowrap. Missing or invalid sizes fall back to 80 columns; narrow sizes are never inflated. Resizing rebuilds the active menu's pre-rendered frames.
- Long descriptions wrap in scrollback. Active choices use the available width, occupying one clipped line each so menus stay shorter than the viewport, including narrow terminals. Terminals too narrow for a bordered menu use numbered line input.
- Piped input, redirected output, dumb terminals, and very short terminals use numbered line input. `NO_COLOR` and non-TTY output disable styling, even when `FORCE_COLOR` is present. No global color override is installed.

All visible frames are genuine Ink `Box` / `Text` JSX rendered through `renderToString`. The keyboard transport is intentionally small and separate from presentation: Perry 0.5.1520's live Ink `render` / `useInput` experiments accepted keys but could fail to repaint, detach input between menus, or leave raw mode active. Using raw key events plus bounded Ink frames keeps the native interaction reliable. No selector plugin is needed. Checks remain UI-free; structured check results reach presentation through the optional `Terminal.result` seam, preserving existing test terminals.

## Kind socket discovery

Kind needs two socket paths: a **host-reachable API endpoint** for preflight HTTP `/_ping`, and an **engine-side bind source** for the container's `/var/run/docker.sock`. On macOS these are different filesystems. Podman's `machine inspect` reports a forwarded macOS socket, often under `/var/folders`; passing that path to `-v` can fail with `statfs: operation not supported` even when its API probe succeeds.

The utility follows the working `pd:dev` strategy in [`deploy/podman/scripts/common.mjs`](../../deploy/podman/scripts/common.mjs): use the selected engine's `podman info` remote socket path, stripping `unix://`, as the bind source. Rootless UID comes from discovery, never a constant. The selected managed connection is matched to the actual machine using loopback SSH port and identity, including rootful connection names with a `-root` suffix. Kind requires both a successful host ping and read-only `podman machine ssh <machine> 'test -S <engine-path>'`; runtime preflight needs no curl, image pull, or probe container.

`CONTAINER_HOST` selects the Podman client endpoint; `CONTAINER_CONNECTION` selects a named connection. `PODMAN_SOCKET` overrides only the absolute engine-side mount path, matching deployment conventions. An override cannot make a remote target local. On Linux the local Unix endpoint is normally both probe and bind source. Recognized Docker Desktop engines use their VM-side `/var/run/docker.sock`, while the selected host/context socket remains the probe endpoint; unknown macOS VM providers have no guessed mount path and block Kind. Docker Desktop behavior is covered by fixtures, not a live Desktop test.

Checks and launch generation share refreshed discovery after every recheck, including remediation and basic-mode fallback. Missing mount paths block Kind and command generation refuses to substitute the host socket or a default. Basic AIO remains available when only Kind prerequisites fail.

Verification on macOS Podman: native PTY `--with-kind` passed all seven checks, skipped persistence, previewed the engine-side mount, and exited 0 after declining launch and copy. An isolated `--rm --pull=never --network none` container using a cached FleetShift image confirmed the mounted destination was a Unix socket and returned HTTP 200 / `OK` from `/_ping`. The probe required `--security-opt label=disable` because SELinux denied socket access under the default container label; generated Kind commands already use the documented `--privileged` mode. No full AIO stack was started by verification.

## Perry native build

Prerequisites are Git, Node.js with npm, workspace dependencies (`npm ci` from the repository root), and a native C compiler/linker with the platform SDK. The macOS build used Xcode Command Line Tools (`xcode-select -p`); Linux needs native compiler/linker tools and system development headers, but its build remains unverified.

From the repository root:

```sh
npx nx run aio-installation-check:build-native
./client/aio-installation-check/dist/native/aio-installation-check --with-kind
```

`build-native` depends on the cacheable `prepare-native-toolchain` target. Setup manages the ignored package-local `.native-toolchain/{perry,cargo,rustup}` directories: it clones the tag matching the exact `@perryts/perry` npm devDependency pin (currently `v0.5.1520`), reads the Rust pin from that checkout's `external-tools.json` (currently `nightly-2026-08-20`), and installs Rustup and the pinned toolchain noninteractively with the minimal profile. No manual checkout, Rust installation, `.env` setup, or Rust exports are needed; setup does not change shell profiles or global Rust installations.

The setup target has `cache: true`, keyed by the setup/build scripts, Perry pin, host platform, architecture, OS release, libc, and absolute project path. Its output is `.native-toolchain`. `build-native` remains `cache: false` and reuses Perry/Cargo archives and compiler caches for incremental builds. The compiler comes from npm; it is not built from source. Perry builds missing feature-matched runtime, standard-library, and native extension support archives on demand, including those needed by the Unix-socket probe.

Build subprocesses use the managed Rust/Cargo homes and toolchain. Perry telemetry is forced off, and `PERRY_NO_AUTO_OPTIMIZE` is removed entirely because its presence, even with an empty value, prevents the coherent `net`/`ws` archive rebuild. Missing `objcopy`/`llvm-objcopy` currently produces non-fatal size-optimization warnings; neither tool is required for a successful build.

The resulting executable runs directly; Node.js is a build dependency, not its launcher. Host Node.js detection inside preflight remains guidance for `fleetctl`.

### Build-only Yoga resolution

`src/native/yoga-layout` is a private local TypeScript package implementing the layout subset used by this utility: row/column flex, fixed dimensions, borders, padding, margins, alignment, and fixed-height clipped selection rows. It is not upstream native Yoga. The terminal transport supplies frame width to Ink's `renderToString` and viewport height to the bounded selection presentation; this subset calculates child geometry within that supplied width. Geometry tests compare representative bordered/wrapped panels and clipped rows with upstream Yoga, including usable widths of 47, 79, 159, and 239 columns (physical terminals of 48, 80, 160, and 240 columns). Unsupported flex directions/wrapping fail explicitly. This remains a UI-specific subset, not a general Yoga replacement.

Perry's built-in `perry/yoga` Taffy backend was evaluated first. Repeated rich Ink layouts failed when moving GC ran during measurement; increasing the nursery postponed the failure but is not a valid fix. A source-level rooting experiment also required rebuilding a matching compiler because Perry rejects modified runtime archives. The final build uses the validated TypeScript subset with the pinned, unmodified compiler/runtime; no GC settings or Perry source patches are required.

`scripts/build-perry.mjs` constructs this disposable resolution layout:

```text
dist/perry/
  src/                       copied AIO source, including main.tsx
  node_modules/
    ink/                     copied installed Ink package
    yoga-layout/             copied local TypeScript package
```

Perry compiles `dist/perry/src/main.tsx` directly. AIO imports resolve the copied Ink; its Yoga imports resolve the adjacent local package. Remaining dependencies resolve upward into the npm workspace graph. Ink is copied rather than symlinked because package resolvers follow symlinks to their real location. Merely declaring an AIO-local Yoga dependency cannot redirect imports from hoisted Ink.

The script refreshes staged source/packages, checks resolution before compilation, and preserves compiler caches. Installed package files are never patched, and normal `fleetctl`/Node Ink imports continue to use upstream Yoga. No Rspack bundle or root npm override is involved.

### Verification and timing

```sh
npm exec -- nx run-many --projects=aio-installation-check --targets=test,lint,typecheck
npm exec -- nx run aio-installation-check:native-verify
```

`native-verify` builds the executable and runs the Node-based PTY acceptance suite. It uses `node-pty`, `@xterm/headless`, and `zx`; it does not require Python or pyte and never approves a fix or starts an AIO container.

The build-resolution regression test covers hoisted Ink, a nested upstream Yoga dependency, upward resolution of other dependencies, unchanged installed files, and removal of stale staged source.

Native PTY verification covers arrow-key focus changes, Enter confirmation, all seven Podman `--with-kind` checks (including the live Unix-socket probe and Kind network), setup with persistence skipped, command preview, declining both Run and Copy, cancellation, text editing, and numeric pipe input. Responsive-width verification at 160 and 48 physical columns confirms 159- and 47-column menu frames, repeated shrink/grow redraws, and exit code 0 with the original terminal state restored. The same launch preview occupies three lines at 160 columns and eight at 48; its widest wide-terminal line is 154 columns. Repeated arrow updates measured approximately 1–18 ms, with no duplicate active frames, staircase output, alternate screen, or terminal-history clearing.

Historical Nx task times before the managed bootstrap, with Rust archives already cached: initial staged graph compilation approximately two minutes, follow-up compilation/link 33 seconds, unchanged warm build 0.56 seconds (1.73 seconds including npm/Nx startup). These are not cold-bootstrap timings. A new Rust feature set can require several minutes on its first build; managed Perry/Cargo caches are retained for reuse.

The managed bootstrap and bare `npx nx run aio-installation-check:build-native` command are verified on macOS using the managed source checkout and pinned `nightly-2026-08-20`, producing an approximately 42 MB executable. The initial cold archive build hit an external timeout; the retry succeeded. Full uninterrupted cold-build timing is not established. A repeated Nx setup run hit the cache; full cache restoration has not yet been verified. Earlier native and PTY results above describe the pre-bootstrap PoC path.

Linux runtime verification and execution with Node absent from PATH remain outstanding. ScriptC was evaluated and its compiler dependency and targets were removed; see the implementation decisions for findings. Compiler warnings about optional React devtools, browser globals, and unused dynamic-code paths remain; the verified native preflight flow completes successfully.
