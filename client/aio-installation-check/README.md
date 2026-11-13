# AIO installation check

Standalone Ink preflight utility for OCM Fleet's all-in-one image, with bordered readiness panels and inline keyboard menus. The specification in [`docs/specs/aio-installation-check.md`](../../docs/specs/aio-installation-check.md) records Perry 0.5.1520 as the validated PoC compiler. ScriptC was evaluated and removed from the current PoC build; rationale appears in the [implementation decisions](../../docs/specs/aio-installation-check-decisions.md).

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

Verification on macOS Podman: native PTY `--with-kind` passed all seven checks, skipped persistence, previewed the engine-side mount, and exited 0 after declining launch and copy. An isolated `--rm --pull=never --network none` container using a cached OCM Fleet image confirmed the mounted destination was a Unix socket and returned HTTP 200 / `OK` from `/_ping`. The probe required `--security-opt label=disable` because SELinux denied socket access under the default container label; generated Kind commands already use the documented `--privileged` mode. No full AIO stack was started by verification.

## Perry native build

Prerequisites are Git, Node.js with npm, workspace dependencies (`npm ci` from the repository root), and a native C compiler/linker with the platform SDK. The macOS build used Xcode Command Line Tools (`xcode-select -p`). Linux requires glibc, system development libraries, and `tar`/`xz`; musl is unsupported. The Fedora 44 arm64 container used for compiler verification installed:

```sh
sudo dnf install nodejs npm git gcc gcc-c++ make cmake clang-devel llvm-devel zlib-devel libzstd-devel libxml2-devel openssl-devel libffi-devel tar xz which
```

`libffi-devel` is needed for the system libraries reported by Fedora's `llvm-config`. These prerequisites were for compiler verification; the Linux x86_64 application build is established by CI instead.

From the repository root:

```sh
npx nx run aio-installation-check:build-native
./client/aio-installation-check/dist/native/aio-installation-check --with-kind
```

`build-native` depends on the cacheable `prepare-native-toolchain` target. Setup manages the ignored package-local `.native-toolchain/{perry,cargo,rustup}` directories: it clones the tag matching the exact `@perryts/perry` npm devDependency pin (currently `v0.5.1520`), reads the Rust pin from that checkout's `external-tools.json` (currently `nightly-2026-08-20`), and installs Rustup and the pinned toolchain noninteractively with the minimal profile. No manual checkout, Rust installation, `.env` setup, or Rust exports are needed; setup does not change shell profiles or global Rust installations.

`.native-toolchain` is ignored by Git and Nx, but ESLint reads neither `.gitignore` nor `.nxignore`, and the shared `fmt`/`fix` targets run `eslint .` from the package root. The shared ESLint configuration therefore excludes `.native-toolchain` explicitly so those targets do not traverse the managed checkout and caches; root-level regression tests cover the exclusion.

On Linux, setup builds the compiler from that same clean pinned checkout using `cargo build --locked --profile perry-dev -p perry --no-default-features --features dev-cli`. It caches `.native-toolchain/compiler/perry` and `provenance.json`, recording source, Rust, LLVM, build environment, and executable hash. Other platforms retain the npm compiler. This avoids the Linux npm 0.5.1520 runtime source-hash mismatch (`src:85ccd72f…` versus clean tag `src:1764a5ba…`); it is not a hoisting issue, and no compatibility check is bypassed. Perry builds missing feature-matched runtime, standard-library, and native extension support archives on demand from the matching checkout, including those needed by the Unix-socket probe.

Linux setup discovers installed LLVM 22 development libraries through `llvm-config-22`/`llvm-config` or `LLVM_SYS_221_PREFIX`. Otherwise it downloads the official LLVM 22.1.8 x64 or arm64 bundle (approximately 2 GB), verifies its pinned SHA256, and extracts it into `.native-toolchain/llvm`. The bundle does not replace the C/linker SDK, system development libraries, or `tar`/`xz` prerequisites.

Official LLVM bundles may report build-time distro paths such as `/usr/lib/x86_64-linux-gnu/libzstd.a` that do not exist on Fedora 44. The pinned `llvm-sys` accepts an absolute `.a` path when it exists, but panics confusingly when it is missing. `prepare-linux-toolchain.mjs` detects missing absolute system-library references and resolves installed host `.so` or `.a` files through `cc -print-file-name`. Validated replacements are applied by the managed `.native-toolchain/llvm-config-adapter` only to known tokens in `--system-libs` output; normal flags and existing valid paths pass through unchanged. LLVM, Perry source/runtime, and compiler compatibility checks remain unmodified.

If the host SDK is missing, setup names the required development package (for example, Fedora's `libzstd-devel`) without making global changes. The verified LLVM bundle is retained, avoiding another approximately 2 GB download. Sync the repository and rerun the same Nx command without cleanup; install any reported missing SDK package before retrying.

The setup target has `cache: true`, keyed by the setup/build scripts, Perry pin, host platform, architecture, OS release, libc, and absolute project path, plus Linux compiler environment and external LLVM identity, including resolved host libraries. Outputs include the managed checkout, Rust toolchain, Linux compiler/provenance, downloaded LLVM, and managed adapter. Library resolution is recorded in compiler provenance. `build-native` remains `cache: false`; Cargo intermediates stay in `.native-toolchain/build` for incremental reuse.

Build subprocesses use the managed Rust/Cargo homes and toolchain. Perry telemetry is forced off, and `PERRY_NO_AUTO_OPTIMIZE` is removed entirely because its presence, even with an empty value, prevents the coherent `net`/`ws` archive rebuild. Missing `objcopy`/`llvm-objcopy` currently produces non-fatal size-optimization warnings; neither tool is required for a successful build.

The resulting executable runs directly; Node.js is a build dependency, not its launcher. Builds target the host CPU architecture and, on Linux, its glibc/distribution baseline; they are not universally portable Linux binaries. Host Node.js detection inside preflight remains guidance for `fleetctl`.

### Build-only Yoga resolution

`src/native/yoga-layout` is a private local TypeScript package implementing the layout subset used by this utility: row/column flex, fixed dimensions, borders, padding, margins, alignment, and fixed-height clipped selection rows. It is not upstream native Yoga. The terminal transport supplies frame width to Ink's `renderToString` and viewport height to the bounded selection presentation; this subset calculates child geometry within that supplied width. Geometry tests compare representative bordered/wrapped panels and clipped rows with upstream Yoga, including usable widths of 47, 79, 159, and 239 columns (physical terminals of 48, 80, 160, and 240 columns). Unsupported flex directions/wrapping fail explicitly. This remains a UI-specific subset, not a general Yoga replacement.

Perry's built-in `perry/yoga` Taffy backend was evaluated first. In the pinned 0.5.1520 it failed rich Ink layouts when Perry's moving GC ran during measurement; upstream fixed that after the pin ([commit 664f111](https://github.com/PerryTS/perry/commit/664f11117753), [PR #11195](https://github.com/PerryTS/perry/pull/11195)).

Newer-release research on [Perry 0.5.1665](https://github.com/PerryTS/perry/releases/tag/v0.5.1665), which is not adopted for this application, re-checked the alternatives. The native backend can match Node layout byte-for-byte given a trivial explicit-column change to the app's row boxes, but the required adapter is larger than the subset (416 versus 305 lines) and stubs `getComputedMargin` and `getDisplay`, so it is not simpler. Upstream `yoga-layout`'s WebAssembly build still fails at startup on a wasm-host `Memory`-export bug that [PR #13940](https://github.com/PerryTS/perry/pull/13940) does not fix.

The build keeps the validated TypeScript subset with the pinned, unmodified compiler/runtime; no GC settings or Perry source patches are required. The [decision note](../../docs/specs/aio-installation-check-decisions.md#compatibility-adapters-and-shims) carries the detailed sizing, stability, and re-evaluation rationale, and its [newer-Perry review](../../docs/specs/aio-installation-check-decisions.md#build-time-rust-and-native-code) records the upgrade verdict.

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
npx nx run aio-installation-check:verify
npx nx run aio-installation-check:native-verify
```

`verify` is the recommended normal verification target: it formats the project first, then runs its lint, format, test, and type checks. `native-verify` stays a separate, explicit step; it builds the executable and runs the Node-based PTY acceptance suite. It uses `node-pty`, `@xterm/headless`, and `zx`; it does not require Python or pyte and never approves a fix or starts an AIO container. The PTY suite passes on the pinned `v0.5.1520` baseline; see the node-pty troubleshooting below for the helper executable bit that a fresh `npm ci` resets.

#### node-pty `posix_spawnp` troubleshooting (macOS)

The `node-pty@1.1.0` npm tarball ships its macOS prebuilt helper without the executable bit: `prebuilds/darwin-arm64/spawn-helper` and `prebuilds/darwin-x64/spawn-helper` are mode 0644 in the registry tarball. node-pty executes that helper for every PTY spawn, so any command, even `/bin/echo`, fails with `posix_spawnp`. The helper is a generated, hoisted workspace dependency; restoring the bit locally with `chmod +x node_modules/node-pty/prebuilds/darwin-arm64/spawn-helper` (or the `darwin-x64` equivalent) makes PTY spawns work again until the next `npm ci`. No root postinstall hook repairs this automatically. Only the Node-based PTY harness is affected; the AIO executable does not use node-pty.

The build-resolution regression test covers hoisted Ink, a nested upstream Yoga dependency, upward resolution of other dependencies, unchanged installed files, and removal of stale staged source.

Native PTY verification covers arrow-key focus changes, Enter confirmation, all seven Podman `--with-kind` checks (including the live Unix-socket probe and Kind network), setup with persistence skipped, command preview, declining both Run and Copy, cancellation, text editing, and numeric pipe input. Responsive-width verification at 160 and 48 physical columns confirms 159- and 47-column menu frames, repeated shrink/grow redraws, and exit code 0 with the original terminal state restored. The same launch preview occupies three lines at 160 columns and eight at 48; its widest wide-terminal line is 154 columns. Repeated arrow updates measured approximately 1–18 ms, with no duplicate active frames, staircase output, alternate screen, or terminal-history clearing.

Historical Nx task times before the managed bootstrap, with Rust archives already cached: initial staged graph compilation approximately two minutes, follow-up compilation/link 33 seconds, unchanged warm build 0.56 seconds (1.73 seconds including npm/Nx startup). These are not cold-bootstrap timings. A new Rust feature set can require several minutes on its first build; managed Perry/Cargo caches are retained for reuse.

The managed bootstrap and bare `npx nx run aio-installation-check:build-native` command are verified on macOS using the managed source checkout and pinned `nightly-2026-08-20`, producing an approximately 42 MB executable. The initial cold archive build hit an external timeout; the retry succeeded. Full uninterrupted cold-build timing is not established; a clean rebuild from the managed source with Cargo caches retained completed the full build in about 5 minutes 17 seconds. A repeated Nx setup run hit the cache; full cache restoration has not yet been verified. Earlier native and PTY results above describe the pre-bootstrap PoC path.

A verification pass on 2026-11-13 completed the clean managed-source macOS build through Nx, passed `npx nx run aio-installation-check:verify` (137 Vitest and 35 Node-based tests), and passed the root ESLint-exclusion regression tests. A separate smoke run of the executable with an empty `PATH`, with no Node.js and no runtime clients visible, still performed runtime discovery and rendered the OCM Fleet branding with explicit missing-runtime-client guidance, exiting 0. The utility currently exits 0 when no runtime is present, so this smoke demonstrates Node-absent startup and rendering, not a completed Node-absent acceptance run.

`npx nx run aio-installation-check:native-verify` passed via Nx on 2026-11-13 in approximately 1 minute 27 seconds on the pinned `v0.5.1520` baseline, after verifier-only fixes with no application changes: the headless screen opts into `@xterm/headless` 6's proposed buffer API, menu border assertions anchor on the bordered box containing the focus marker rather than the banner box above it, and a repaint counts as complete only once the focus marker exists again instead of reading the briefly erased mid-redraw line as a torn frame. The recorded evidence in `dist/native-terminal-evidence/results.json` covers three full flows at 80x24, 48x18, and 80x24 with `NO_COLOR`, 203 arrow inputs each with maximum update times of 23, 23, and 40 ms and exit 0; Ctrl-C and SIGTERM cancellation both exit 130 with the cursor restored; the piped numeric full-decline flow passes; EOF exits 1 with an explicit error; and the container inventory is unchanged. No container was started and no launch was accepted.

Real CI passed the pinned `v0.5.1520` path on Linux x86_64 on 2026-11-13 ([run 43411017880](https://github.com/stolostron/ocm-fleet/actions/runs/43411017880), commit `aebbc33`, job approximately 18 minutes 18 seconds): a cold `build-native` with no remote cache, an ELF host-architecture check, an isolated Node-free and client-free startup smoke, and an unchanged tracked tree. `npx nx run aio-installation-check:smoke-native` reproduces that smoke locally: it runs the built executable from a private empty `PATH`, verifies Node is unreachable, checks the 64-bit ELF matches the host architecture, and asserts the branding, runtime-discovery, and explicit no-client markers, recording evidence under `dist/native-smoke-evidence/`. This proves Linux x86_64 build and Node-absent startup and discovery only; full Linux interactive runtime/launch checks, engine-backed Kind checks on Linux, and complete Node-absent launch acceptance remain outstanding.

On Fedora 44 arm64, the source compiler and AIO executable built successfully after installing the required `libffi-devel`. The resulting ARM64 ELF executable is approximately 42.6 MB. A direct execution rendered the readiness screen and reported the container's missing Podman/Docker clients. Full interactive runtime/launch checks and full Node-absent acceptance remain outstanding on Linux.

Adapter verification on Fedora 44 ARM64 injected the exact missing `/usr/lib/x86_64-linux-gnu/libzstd.a` token alongside real LLVM 22 output. A raw Cargo `llvm-sys` check reproduced the panic; the managed adapter normalized it to `-lzstd`, resolving the real `/usr/lib64/libzstd.so`. Both Cargo check and the real source-built Perry linker passed. This supplements the earlier ARM64 application build/smoke check; the x86_64 application build is verified in CI.

Upstream unused-item warnings from the `dev-cli` compiler build are non-blocking. Warnings about pthread declarations remain unresolved. Earlier optional React devtools, browser-global, and unused dynamic-code warnings did not block the verified macOS preflight flow. ScriptC was evaluated and its compiler dependency and targets were removed; see the implementation decisions for findings.
