# AIO installation-check PoC: implementation decisions

This note explains why the AIO installation check is a native executable, why
the PoC uses Perry rather than ScriptC, why it keeps Ink as its presentation
layer, and what native-build adapters are present. Normative behavior remains
in the [AIO installation-check specification](./aio-installation-check.md).

## Decision summary

| Decision                                                            | Rationale                                                                                                                                                                                                                                                                                                                                                                                                                | Current status                                                                                                                                                                                                                                        |
| ------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Distribute the utility as a native executable                       | Host readiness must be checkable before users install Node.js. Node guidance is for `fleetctl`, not a prerequisite for checking or launching AIO.                                                                                                                                                                                                                                                                        | PoC executable builds and runs on macOS. Fedora 44 ARM64 build and readiness smoke check passed; CI verified the Linux x86_64 cold build and Node-free startup; full Linux runtime checks and full run-without-Node acceptance remain to be verified. |
| Use Perry 0.5.1520 for the PoC native build                         | Perry successfully compiles the TypeScript/Ink dependency graph used by this utility. ScriptC 0.3.1 currently cannot compile the required Ink/React graph and runtime call shapes.                                                                                                                                                                                                                                       | Validated PoC path; compiler for a distributed product remains a release decision.                                                                                                                                                                    |
| Keep Ink/React as the UI layer                                      | `fleetctl` already uses Ink and React. Using the same presentation technology keeps OCM Fleet CLIs aligned and allows future component reuse without tying `fleetctl` to Perry.                                                                                                                                                                                                                                          | Both CLIs use Ink/React; packages currently maintain their own app-specific components.                                                                                                                                                               |
| Keep keyboard/input transport separate from rendering               | Perry's live Ink `render`/`useInput` path did not reliably repaint or clean up terminal state in repeated interactive flows.                                                                                                                                                                                                                                                                                             | AIO redraws bounded Ink-rendered frames from a small terminal input transport.                                                                                                                                                                        |
| Use a private, limited Yoga compatibility module in the Perry build | Perry's native Taffy/Yoga route measures row boxes intrinsically where Yoga constrains to the parent width, overflowing long panel text at narrow terminals; upstream Yoga's WebAssembly route fails at startup (lost `Memory` export for import-instantiated modules). The pinned 0.5.1520 also had a moving-GC measurement failure, fixed upstream after the pin. The current panels use a small, known layout subset. | Staged only for the AIO Perry build; not a general Yoga replacement and not used by `fleetctl`.                                                                                                                                                       |

## Why native execution

This utility is meant to help prepare a host to run OCM Fleet AIO. Requiring
Node.js to launch the checker would make the checker depend on a separate
runtime before it can tell the user whether their container runtime is ready.
That is particularly awkward because Node.js is guidance for `fleetctl`, while
missing Node.js must not block AIO preflight or launch setup.

A native executable moves the JavaScript/TypeScript runtime dependency to the
build process. It is still a platform-specific artifact: macOS and Linux need
their own builds, release packaging and architecture coverage remain to be
validated, and execution with Node absent from `PATH` is verified at the
startup and discovery level only. Native here means “no
separately installed Node.js required to invoke the built utility”; it does not
mean “one binary works on every operating system” or “the build needs no
toolchain.” Builds target the host CPU architecture and, on Linux, its glibc/distribution baseline; release portability requires separate validation.

## Why ScriptC did not become the PoC compiler

ScriptC 0.3.1 was the initial compiler candidate. The application-specific
checks and flow were mostly within its static-compilation coverage, but the
reachable Ink 6.8 / React 19 dependency graph was not. Reported blockers
included React reconciler and scheduler declarations, JSON loading in
`cli-boxes`, package-subpath imports in Chalk, optional React DevTools loading,
and CommonJS/package-resolution shapes. Selecting packages as static did not
remove blockers in their JavaScript implementation.

Bundling with Rspack did not solve this compiler boundary: ScriptC rejected the
bundle with errors across React reconciler/runtime code and transformed module
shapes. Downgrading Ink was also explored; candidate versions still depended on
the problematic React/reconciler stack and did not provide the needed
`renderToString` entry point.

Some Node APIs used by the utility also lack ScriptC lowering for the required
call shapes. In particular, Unix-socket HTTP probing through Node's
`socketPath`/`net.createConnection({ path })` is unsupported. Relevant
terminal, subprocess, and filesystem call shapes also need ScriptC-specific
validation.

These are compatibility gaps between the evaluated compiler and the selected
dependency/API graph, not evidence that Ink or native execution is unnecessary.
The ScriptC experiment is closed for this PoC; its dependency and build targets
have been removed. Revisit it only as a separately scoped compiler evaluation.

## Why Perry works, with adaptations

Perry 0.5.1520 compiles the source and dependency graph used by the PoC into a
native executable. The successful path compiles the TypeScript sources directly
rather than relying on a Node-launched wrapper or a generated Rspack bundle.
Perry also provides the native `net` support used by the AIO Unix-socket probe.

Perry does not make every Ink interaction path reliable automatically. The
PoC uses Ink's `Box` and `Text` components and `renderToString` for presentation,
then handles terminal input and bounded redraws separately. This preserves the
same Ink/React presentation model used by `fleetctl` while avoiding the live
Perry/Ink render and `useInput` behavior that failed to repaint consistently or
clean up input state in repeated TTY interactions.

The explicit `react/jsx-runtime` import in the AIO terminal module is also
intentional. Perry otherwise selected eager native JSX lowering for this
entrypoint, which produced invalid hook/context behavior. The import makes JSX
lower to React elements for Ink's renderer.

### Why not Perry's TUI directly?

Using Perry's own TUI would introduce a second presentation framework alongside
Ink/React in OCM Fleet's command-line tools. That would mean separate
components, styling conventions, and interaction behavior for the installation
checker, even though the checker and `fleetctl` belong to the same CLI
experience.

The extra native transport and layout compatibility code is bounded and local
to this executable. The AIO utility's Yoga subset is about 300 lines and covers
only layouts currently used by its panels and choices. PTY checks measured
arrow-key redraws at approximately 1–18 ms and verified resize, cancellation,
scrollback preservation, and terminal-state restoration. The renderer adds no
separate process, service, or network hop. These checks indicate acceptable PoC
behavior; they are not a general benchmark or a guarantee for every terminal.

Ink is a convenience choice for deduplication and potential component reuse, not a permanent architectural commitment. This bootstrap CLI is expected to stay small and avoid excessive complexity. If future Ink/Perry compatibility work or other blockers cost more to resolve than sharing the presentation layer is worth, we can switch to Perry-native UI and accept maintaining a separate small UI.

## Build-time Rust and native code

The application logic, checks, UI, and orchestration are TypeScript. Rust is a build-time dependency of the Perry native toolchain, not a language used for the AIO checker logic. Setup automatically selects the Rust pin from the matching Perry checkout's `external-tools.json`, currently `nightly-2026-08-20`; its Cargo config uses a nightly-only Cargo option.

`build-native` depends on `prepare-native-toolchain`, which manages the ignored package-local `.native-toolchain/{perry,cargo,rustup}` directories. It clones the tag matching the exact `@perryts/perry` npm devDependency pin (currently `v0.5.1520`) and installs Rustup and the pinned toolchain noninteractively with the minimal profile. This requires no manual checkout, Rust installation, `.env` setup, or Rust exports, and does not change shell profiles or global Rust installations.

The published Perry npm package does not include every prebuilt support archive needed for this feature set. Perry uses the matching checkout and managed Rust/Cargo toolchain to build feature-matched runtime, standard-library, and native extension support archives on demand, including the `net` support needed by the socket probe, then links the application into the executable.

Linux builds the compiler from that same clean pinned Git checkout with `cargo build --locked --profile perry-dev -p perry --no-default-features --features dev-cli`; other platforms retain the npm compiler. The Linux npm 0.5.1520 compiler embeds runtime source hash `src:85ccd72f…`, while the clean tag yields `src:1764a5ba…`. This content mismatch is not caused by npm hoisting. Building both compiler and support archives from the same checkout preserves compatibility checks without source-stamp overrides or bypasses.

`scripts/prepare-linux-toolchain.mjs` discovers LLVM 22 development libraries through `llvm-config-22`/`llvm-config` or `LLVM_SYS_221_PREFIX`, including the static libraries required by the compiler. Otherwise it downloads the official LLVM 22.1.8 x64 or arm64 bundle (approximately 2 GB), verifies its pinned SHA256, and installs it into `.native-toolchain/llvm`. Linux requires glibc; musl is unsupported.

Official bundles can retain build-time distro absolute system-library paths, such as `/usr/lib/x86_64-linux-gnu/libzstd.a` on Fedora 44. The pinned `llvm-sys` accepts an existing absolute `.a` path but produces a confusing panic if it is missing. Bootstrap detects missing absolute references and uses `cc -print-file-name` to resolve and validate installed host `.so` or `.a` files. A managed adapter corrects only those known tokens in `--system-libs` output, preserving normal flags and existing valid paths. Real LLVM and Perry source/runtime stay unmodified, with compiler compatibility checks intact.

Missing host libraries produce actionable SDK guidance, such as Fedora's `libzstd-devel`; setup makes no global changes and retains the verified bundle so a retry does not repeat the approximately 2 GB download. Users can sync and rerun the same Nx command without cleanup, installing any reported missing SDK package before retrying.

The setup target has `cache: true`. Outputs include the managed checkout and Rust toolchain, `.native-toolchain/compiler/{perry,provenance.json}`, downloaded LLVM, and `.native-toolchain/llvm-config-adapter`. Compiler reuse checks provenance (source, Rust, LLVM, resolved host libraries, environment, build arguments), executable hash/version, and checkout cleanliness. Cache keys include the setup/build scripts, Perry pin, host platform, architecture, OS release, libc, absolute project path, and Linux compiler environment/external LLVM identity, including host-library resolution. `build-native` remains `cache: false`; Cargo intermediates in `.native-toolchain/build` are retained outside the cached outputs for incremental reuse. Build subprocesses use managed Rust/Cargo homes, force Perry telemetry off, and remove `PERRY_NO_AUTO_OPTIMIZE` entirely: its presence, even with an empty value, disables the coherent `net`/`ws` archive rebuild.

Git, Node.js with npm, workspace dependencies, and a platform C/linker toolchain and SDK remain prerequisites. Linux also requires system development libraries and `tar`/`xz`, even with the LLVM bundle. The macOS build used Xcode Command Line Tools; the [package README](../../client/aio-installation-check/README.md#perry-native-build) lists the Fedora 44 arm64 prerequisites used for compiler verification, including `libffi-devel` for `llvm-config`'s system libraries. The intended user entrypoint is the produced executable, not Cargo. Missing `objcopy`/`llvm-objcopy` causes non-fatal size-optimization warnings, not build failure.

A review of newer Perry releases (research on
[Perry 0.5.1665](https://github.com/PerryTS/perry/releases/tag/v0.5.1665), npm
latest, published 2026-11-05) found the managed toolchain's hardest parts
retired upstream: [commit fd766e9](https://github.com/PerryTS/perry/commit/fd766e92)
makes runtime compatibility compare archive versions instead of the strict
source stamp, newer releases link prebuilt runtime, standard-library, and
extension archives instead of building them with a compile-time Rust toolchain,
and the npm compiler embeds LLVM in-process. An experimental upgrade removes
the managed Rust/LLVM toolchain but is not adopted here. With that upgrade,
the full AIO application compiles via Nx on native Linux ARM64 (Fedora
44 and UBI9, clang 22.1.8 with GNU binutils, roughly 40.7-41.4 MB aarch64
executables), and a Node-free smoke with no container-runtime clients passes;
native PTY menus render inside those containers, but the full PTY script suite
has not run there (no engine host socket), so full Linux interactive use
remains unvalidated.

On macOS the 0.5.1665 npm compiler fails to link the full application with
832-850 duplicate rustls/std/core symbols between the prepared `net` extension
wrapper and the fetched full standard library; the failure is deterministic
across fresh caches and with `llvm-objcopy` present, persists with the optional
React DevTools dependency excluded because the `net` extension is required, and
is independent of the Yoga choice. The upgrade is therefore not adopted until
upstream fixes the macOS link failure; 0.5.1520 stays pinned because macOS
cannot be dropped. The 0.5.1520 build instructions above remain correct for
the pinned path; retire the managed toolchain design only as part of a
validated upgrade, not by extending the current one.

### Compatibility adapters and shims

| Adapter                                | Used by                      | Purpose and boundary                                                                                                                                                                                                                                                                                                                                                                 |
| -------------------------------------- | ---------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `scripts/llvm-config-adapter.mjs`      | Managed Linux toolchain only | Generated under `.native-toolchain/llvm-config-adapter`; rewrites validated missing system-library tokens only for `--system-libs`, leaving other output and the real LLVM installation unchanged.                                                                                                                                                                                   |
| `src/native/yoga-layout/index.ts`      | Perry native build only      | Implements the Yoga API subset Ink currently needs: row/column layout, dimensions, borders, padding, margins, alignment, and fixed-height clipping. It fails explicitly for unsupported styles. It is not upstream Yoga, not a general layout engine, and does not change normal Node/`fleetctl` resolution.                                                                         |
| Staged Ink and Yoga package copies     | Perry build staging          | `scripts/build-perry.mjs` copies AIO source, Ink, and the local Yoga subset into `dist/perry`. Copying Ink is necessary because symlink resolution would make hoisted Ink continue resolving upstream Yoga from its original package path. A resolution check guards the staged graph. Installed packages are not patched; other dependencies continue resolving from the workspace. |
| `react/jsx-runtime` side-effect import | AIO Perry UI                 | Selects React-element JSX lowering so Ink controls rendering and hooks/context work correctly. This is a runtime-selection hint, not a replacement React implementation.                                                                                                                                                                                                             |

The Yoga subset is the main compatibility shim users may encounter in the
build. It is intentionally narrower than Yoga so layout behavior stays
reviewable and testable. Tests compare representative panel and menu geometry
with upstream Yoga at several terminal widths. Adding new Ink layout styles
requires extending that subset and its parity tests, or revisiting the renderer
choice; unsupported layouts must not silently degrade.

Research against Perry 0.5.1665 (npm latest at the time; the application stays
pinned to 0.5.1520) re-checked both alternatives and keeps the subset. The
moving-GC measurement failure that originally motivated it is fixed upstream
after the pin ([commit 664f111](https://github.com/PerryTS/perry/commit/664f11117753),
[PR #11195](https://github.com/PerryTS/perry/pull/11195)); 800 rich Ink
`renderToString` calls stayed byte-identical across nine runs including forced
evacuation. The built-in Taffy 0.13 backend still measures ink's default row
`Box` with a measured `Text` leaf by intrinsic `MaxContent` sizing instead of
the parent's constrained `AT_MOST` width, so long panel reasons overflow at
usable widths of 47 and 79 columns (159 and 239 match), identically to 0.5.1520.
A bounded follow-up validated an escape route rather than an unavoidable
limitation: a small explicit `flexDirection="column"` patch on the row boxes
holding wrapped text is layout-neutral under Node, and with it the built-in
backend behind a thin adapter rendered the real UI byte-identically to Node at
all four widths under GC stress. The subset stays and no UI patch was adopted:
the tested adapter is about 416 lines against the subset's 305, forwards styles
silently instead of failing explicitly on unsupported ones, and stubs
`getComputedMargin` and `getDisplay`, so it does not reduce compatibility
maintenance.
Upstream `yoga-layout`'s WebAssembly build compiles but fails at startup
because the wasm host drops `WebAssembly.Memory`-typed exports for modules
instantiated with an import object; an isolated reproduction confirms it and
[PR #13940](https://github.com/PerryTS/perry/pull/13940) does not fix it.
Re-evaluate the subset when either blocker is fixed and released.

## Build and verification path

From the repository root, install workspace dependencies with `npm ci`, then run:

```sh
npx nx run aio-installation-check:build-native
./client/aio-installation-check/dist/native/aio-installation-check --with-kind
```

The build target prepares the managed toolchain, stages sources and dependencies, validates Ink/Yoga module resolution, and invokes the source-built Linux compiler or npm compiler on other platforms. It preserves Perry/Cargo caches for incremental builds. `npx nx run aio-installation-check:native-verify` builds the executable and runs the Node-based PTY acceptance suite using `node-pty`, `@xterm/headless`, and `zx`. Normal source verification is `npx nx run aio-installation-check:verify`, which formats the project and then runs its lint, format, test, and type checks. The PTY suite passes on the pinned 0.5.1520 baseline once node-pty's non-executable macOS `spawn-helper` prebuild bit is restored; the [package README](../../client/aio-installation-check/README.md#verification-and-timing) documents the workaround that a fresh `npm ci` resets.

The managed bootstrap and bare `npx nx run aio-installation-check:build-native` command are verified on macOS using the managed source checkout and pinned `nightly-2026-08-20`, producing an approximately 42 MB executable. The initial cold archive build hit an external timeout; the retry succeeded. Full uninterrupted cold-build timing is not established; a clean rebuild from the managed source with Cargo caches retained completed the full build in about 5 minutes 17 seconds. A repeated Nx setup run hit the cache; full cache restoration has not yet been verified. A verification pass on 2026-11-13 also passed `npx nx run aio-installation-check:verify` (137 Vitest and 35 Node-based tests) and the root ESLint-exclusion regression tests, and `npx nx run aio-installation-check:native-verify` passed in approximately 1 minute 27 seconds on the pinned 0.5.1520 baseline after verifier-only fixes, recording three full flows, exit-130 cancellation with cursor restoration, piped numeric decline, EOF exit 1, and an unchanged container inventory; no container was started and no launch was accepted. A separate macOS smoke run with an empty `PATH` and no Node.js available performed runtime discovery and rendered the branding with explicit missing-runtime-client guidance; the utility currently exits 0 when no runtime is present, so this is Node-absent startup evidence, not completed acceptance. Real CI passed the same pinned path on Linux x86_64 on 2026-11-13 (cold build, Node-free startup smoke, unchanged tracked tree); the [package README](../../client/aio-installation-check/README.md#verification-and-timing) links the run and the smoke command.

Before the managed bootstrap, a macOS native PTY flow passed all seven Podman Kind checks, showed
the launch preview with the VM-side socket, and exited cleanly after declining
launch. An isolated cached-image bind probe confirmed the socket mount and
Engine API `/_ping` response. No full AIO stack was started by those checks.

On Fedora 44 arm64, the source compiler and approximately 42.6 MB ARM64 ELF application built successfully with the required `libffi-devel` installed. Direct execution rendered readiness and reported missing Podman/Docker clients in the container. Upstream unused-item warnings from `dev-cli` are non-blocking; pthread declaration warnings remain unresolved.

Separately, Fedora 44 ARM64 adapter verification injected the exact missing `/usr/lib/x86_64-linux-gnu/libzstd.a` token alongside real LLVM 22 output. Raw Cargo `llvm-sys` checking reproduced the panic; the managed adapter emitted `-lzstd`, resolving the real `/usr/lib64/libzstd.so`. Cargo check and the real source-built Perry linker both passed. This verifies the path correction, alongside the earlier ARM64 application build/readiness smoke check; the x86_64 application build is verified in CI.

Still outstanding: full Linux interactive runtime/launch checks, engine-backed Kind checks on Linux, full Node-absent acceptance (empty-`PATH` startup smokes and the CI Linux x86_64 run cover startup and discovery only), and live Docker Desktop validation. Current evidence validates the macOS PoC path, the Fedora ARM64 build/readiness smoke check, and the CI Linux x86_64 cold build with Node-free startup, not a completed distribution matrix.

## References

- [AIO installation-check specification](./aio-installation-check.md)
- [AIO package build and terminal verification notes](../../client/aio-installation-check/README.md)
- [`fleetctl` Ink/React UI](../../client/cli/src/ui.tsx)
- [Perry native build staging](../../client/aio-installation-check/scripts/build-perry.mjs)
- [Private Perry Yoga subset](../../client/aio-installation-check/src/native/yoga-layout/index.ts)
- [Unix-socket probe](../../client/aio-installation-check/src/checks/unix-socket-probe.ts)
