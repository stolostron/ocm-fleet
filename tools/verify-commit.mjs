import { execFileSync, spawn } from "node:child_process";
import { randomUUID } from "node:crypto";
import {
  closeSync,
  existsSync,
  fchmodSync,
  lstatSync,
  openSync,
  readFileSync,
  renameSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import {
  copyFile,
  cp,
  mkdir,
  mkdtemp,
  readFile,
  readdir,
  readlink,
  realpath,
  rm,
  symlink,
  writeFile,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import {
  basename,
  dirname,
  isAbsolute,
  join,
  relative,
  resolve,
  sep,
} from "node:path";
import { fileURLToPath } from "node:url";
import { isDeepStrictEqual } from "node:util";

/**
 * Verify the effective Git index in a disposable linked worktree. Source edits
 * only reach the real checkout after the exact candidate passes all checks.
 * runTarget is injectable so Git transaction tests can use real small processes
 * without repeatedly starting Nx; normal use always runs the workspace targets.
 */
export async function verifyCommit({
  root = process.cwd(),
  env = process.env,
  log = console.log,
  runTarget = runNx,
  signal,
} = {}) {
  root = await realpath(
    git(root, env, ["rev-parse", "--show-toplevel"]).trim(),
  );
  const originalTree = git(root, env, ["write-tree"]).trim();
  const indexPath = git(root, env, [
    "rev-parse",
    "--path-format=absolute",
    "--git-path",
    "index",
  ]).trim();
  const originalIndex = readFileSync(indexPath);
  const head = git(root, env, ["rev-parse", "--verify", "HEAD"], {
    optional: true,
  }).trim();
  const staged = new Set(
    paths(
      git(root, env, [
        "diff",
        "--cached",
        "--name-only",
        "--no-renames",
        "-z",
        ...(head ? [head] : []),
      ]),
    ),
  );
  if (!staged.size) {
    log("pre-commit: no staged source changes to verify.");
    return;
  }

  // Git exports the original index/directory to hooks, including a temporary
  // index for `git commit --only`. Keep that environment for the real index,
  // but clear it for commands and nested Git calls in the private worktree.
  const snapshotEnv = { ...env };
  for (const key of git(root, env, ["rev-parse", "--local-env-vars"])
    .trim()
    .split("\n"))
    delete snapshotEnv[key];
  for (const key of Object.keys(snapshotEnv)) {
    if (
      key.startsWith("NX_TASK_") ||
      [
        "NX_WORKSPACE_ROOT",
        "NX_WORKSPACE_ROOT_PATH",
        "NX_PROJECT_NAME",
        "NX_WORKSPACE_DATA_DIRECTORY",
        "NX_PROJECT_GRAPH_CACHE_DIRECTORY",
      ].includes(key)
    )
      delete snapshotEnv[key];
  }
  snapshotEnv.NX_DAEMON = "false";
  snapshotEnv.NX_DEFAULT_OUTPUT_STYLE ??= "static-failures-only";
  const temporary = await mkdtemp(join(tmpdir(), "ocm-fleet-staged-"));
  const snapshot = join(temporary, "checkout");
  let registered = false;
  let preserveRecovery = false;
  try {
    // An unborn branch needs a commit for worktree add; no branch or ref is
    // created. Existing repositories retain their Git identity and Nx cache.
    const base =
      head ||
      git(
        root,
        {
          ...snapshotEnv,
          GIT_AUTHOR_NAME: "Staged verification",
          GIT_AUTHOR_EMAIL: "verify@example.invalid",
          GIT_COMMITTER_NAME: "Staged verification",
          GIT_COMMITTER_EMAIL: "verify@example.invalid",
        },
        ["commit-tree", originalTree, "-m", "Staged verification"],
      ).trim();
    git(root, snapshotEnv, [
      "-c",
      `core.hooksPath=${join(temporary, "no-hooks")}`,
      "worktree",
      "add",
      "--detach",
      "--no-checkout",
      snapshot,
      base,
    ]);
    registered = true;
    git(snapshot, snapshotEnv, ["read-tree", originalTree]);
    git(snapshot, snapshotEnv, ["checkout-index", "--all", "--force"]);
    await linkDependencies(root, snapshot);

    const eligible = new Map();
    for (const path of staged) {
      const source = regularFile(root, path);
      const checkedOut = regularFile(snapshot, path);
      // Git records the owner's executable bit, not group/other permissions.
      if (sameFile(source, checkedOut, 0o100)) eligible.set(path, source);
    }

    log("pre-commit: checking the staged snapshot (Nx cache enabled).");
    let fixes = [];
    if (!(await runTarget("lint", snapshot, snapshotEnv, signal))) {
      log(
        "pre-commit: trying formatting and import fixes in the private checkout.",
      );
      const formatted = await runTarget("fmt", snapshot, snapshotEnv, signal);
      const changed = changedSources(snapshot, snapshotEnv);
      const unsafe = changed.filter((path) => {
        const after = regularFile(snapshot, path);
        const before = eligible.get(path);
        return !before || !after || ((after.mode ^ before.mode) & 0o100) !== 0;
      });
      if (formatted && !unsafe.length) {
        fixes = changed.map((path) => ({
          path,
          before: eligible.get(path),
          after: {
            ...regularFile(snapshot, path),
            mode: eligible.get(path).mode,
          },
        }));
        git(snapshot, snapshotEnv, ["add", "--update"]);
      } else {
        log(
          `pre-commit: leaving fixes unapplied${unsafe.length ? `; these files have unstaged work or were not staged: ${unsafe.map(JSON.stringify).join(", ")}` : " because formatting failed"}. Checking the original staged contents.`,
        );
        // Only this disposable checkout is reset. No stash or restore operation
        // ever touches the developer's working tree.
        git(snapshot, snapshotEnv, [
          "restore",
          `--source=${originalTree}`,
          "--staged",
          "--worktree",
          ".",
        ]);
        git(snapshot, snapshotEnv, ["clean", "-fd"]);
      }
    }
    const candidateTree = git(snapshot, snapshotEnv, ["write-tree"]).trim();
    if (!(await runTarget("check", snapshot, snapshotEnv, signal)))
      throw new Error(
        "staged checks failed; run npx nx run ocm-fleet:verify, review the fixes, and stage them.",
      );
    signal?.throwIfAborted();
    if (
      changedSources(snapshot, snapshotEnv).length ||
      git(snapshot, snapshotEnv, ["write-tree"]).trim() !== candidateTree
    )
      throw new Error(
        "a read-only check changed source; refusing to publish an unchecked candidate.",
      );
    if (!fixes.length) {
      if (git(root, env, ["write-tree"]).trim() !== originalTree)
        throw new Error(
          "the index changed during verification; retry the commit.",
        );
    } else {
      const candidateIndex = join(temporary, "candidate-index");
      await writeFile(candidateIndex, originalIndex);
      const entries = fixes
        .map(({ path }) =>
          git(snapshot, snapshotEnv, ["ls-files", "--stage", "-z", "--", path]),
        )
        .join("");
      git(
        root,
        { ...env, GIT_INDEX_FILE: candidateIndex },
        ["update-index", "-z", "--index-info"],
        { input: entries },
      );
      // Keep recovery data until publication completes, even on an unexpected
      // filesystem error. Ordinary lint/test failures never get this far.
      await writeFile(join(temporary, "original-index"), originalIndex);
      await writeFile(
        join(temporary, "files.json"),
        JSON.stringify(
          fixes.map(({ path, before, after }) => ({
            path,
            mode: before.mode,
            original: before.bytes.toString("base64"),
            formatted: after.bytes.toString("base64"),
          })),
        ),
      );
      try {
        signal?.throwIfAborted();
        publish(
          root,
          indexPath,
          originalIndex,
          readFileSync(candidateIndex),
          fixes,
        );
      } catch (error) {
        if (error.recoveryNeeded) {
          preserveRecovery = true;
          log(
            `pre-commit: recovery copies are in ${temporary}; do not delete this directory until your work is recovered.`,
          );
        }
        throw error;
      }
      log(
        `pre-commit: formatted and staged ${fixes.map(({ path }) => JSON.stringify(path)).join(", ")}.`,
      );
    }
    log("pre-commit: staged verification passed.");
  } catch (error) {
    // Nx's normal shared cache keeps logs outside this checkout. If it falls
    // back to a checkout-local cache, retain failure logs before removing it.
    const logs = join(snapshot, ".nx/cache/terminalOutputs");
    if (existsSync(logs)) {
      const destination = join(root, ".nx/commit-logs", randomUUID());
      await cp(logs, destination, { recursive: true });
      log(`pre-commit: saved check logs in ${destination}`);
    }
    throw error;
  } finally {
    if (registered) {
      try {
        git(root, snapshotEnv, ["worktree", "remove", "--force", snapshot]);
      } catch (error) {
        preserveRecovery = true;
        log(
          `pre-commit: could not clean up the private worktree at ${snapshot}: ${error.message}`,
        );
      }
    }
    if (!preserveRecovery)
      await rm(temporary, { recursive: true, force: true });
  }
}

/** Run existing Nx policy; keep task definitions out of the hook adapter. */
function runNx(target, cwd, env, signal) {
  return new Promise((resolveResult, reject) => {
    const nxPackage = JSON.parse(
      readFileSync(join(cwd, "node_modules/nx/package.json"), "utf8"),
    );
    const child = spawn(
      process.execPath,
      [
        join(cwd, "node_modules/nx", nxPackage.bin.nx),
        // run-many also collapses passing workspace tasks. A single-project run
        // prints every task in that project, even with failures-only output.
        "run-many",
        `--targets=${target}`,
        "--projects=ocm-fleet",
      ],
      { cwd, env, stdio: "inherit" },
    );
    const abort = () => child.kill("SIGTERM");
    signal?.addEventListener("abort", abort, { once: true });
    if (signal?.aborted) abort();
    child.once("error", reject);
    child.once("exit", (code, termination) => {
      signal?.removeEventListener("abort", abort);
      if (signal?.aborted || termination)
        reject(
          new Error(
            "verification interrupted; source and index were not changed.",
          ),
        );
      else resolveResult(code === 0);
    });
  });
}

/**
 * Reuse installed external dependencies, but redirect npm workspace links and
 * their bins to the staged checkout. Linking the whole node_modules directory
 * would silently resolve workspace imports into the developer's unstaged code.
 * Build outputs, formatter caches and Nx graph data stay checkout-local; Nx's
 * default task cache/database are already shared by linked worktrees.
 */
async function linkDependencies(root, snapshot) {
  if (!existsSync(join(snapshot, "package-lock.json"))) return;
  const lock = JSON.parse(
    await readFile(join(snapshot, "package-lock.json"), "utf8"),
  );
  let installed;
  try {
    installed = JSON.parse(
      await readFile(join(root, "node_modules/.package-lock.json"), "utf8"),
    );
  } catch {
    throw new Error(
      "installed dependencies are missing; run npm ci before committing.",
    );
  }
  const mismatch = () =>
    new Error(
      "staged dependencies differ from the installed dependencies; run npm ci with the intended manifests/lockfile, then retry.",
    );
  const packages = lock.packages;
  if (!packages) throw mismatch();
  for (const [path, info] of Object.entries(packages)) {
    if (!path.includes("node_modules/")) {
      const manifest = JSON.parse(
        await readFile(join(snapshot, path, "package.json"), "utf8"),
      );
      for (const key of [
        "dependencies",
        "devDependencies",
        "optionalDependencies",
        "peerDependencies",
        "workspaces",
      ]) {
        if (!isDeepStrictEqual(manifest[key], info[key])) throw mismatch();
      }
    } else {
      const actual = installed.packages?.[path];
      if (!actual && info.optional) continue; // npm omits other platforms' optional binaries.
      for (const key of ["version", "integrity", "resolved", "link"]) {
        if (!actual || actual[key] !== info[key]) throw mismatch();
      }
    }
  }
  const moduleRoots = new Set(["node_modules"]);
  for (const info of Object.values(packages)) {
    if (info.link) {
      const linked = relative(snapshot, resolve(snapshot, info.resolved));
      if (
        linked === ".." ||
        linked.startsWith(`..${sep}`) ||
        isAbsolute(linked)
      )
        throw new Error(
          "external local npm links cannot be checked as staged source; keep workspace packages inside the repository.",
        );
      moduleRoots.add(join(linked, "node_modules"));
    }
  }
  for (const directory of moduleRoots) {
    if (!existsSync(join(root, directory))) continue;
    await mirrorModules(root, snapshot, directory);
  }
  await copyFile(
    join(root, "node_modules/.package-lock.json"),
    join(snapshot, "node_modules/.package-lock.json"),
  );
}

async function mirrorModules(root, snapshot, directory) {
  await mkdir(join(snapshot, directory), { recursive: true });
  for (const entry of await readdir(join(root, directory), {
    withFileTypes: true,
  })) {
    if (entry.name.startsWith(".") && entry.name !== ".bin") continue;
    const path = join(directory, entry.name);
    if (entry.name === ".bin" || entry.name.startsWith("@")) {
      await mirrorModules(root, snapshot, path);
      continue;
    }
    const source = join(root, path);
    if (basename(directory) === ".bin" && entry.isFile()) {
      // npm uses regular command shims on Windows rather than POSIX symlinks.
      await copyFile(source, join(snapshot, path));
      continue;
    }
    const target = await canonicalPath(
      entry.isSymbolicLink()
        ? resolve(dirname(source), await readlink(source))
        : source,
    );
    const local = relative(root, target);
    const isWorkspace =
      local !== ".." &&
      !local.startsWith(`..${sep}`) &&
      !isAbsolute(local) &&
      !local.split(sep).includes("node_modules");
    await symlink(
      isWorkspace ? join(snapshot, local) : target,
      join(snapshot, path),
      existsSync(target) && lstatSync(target).isDirectory()
        ? "junction"
        : "file",
    );
  }
}

async function canonicalPath(path) {
  try {
    return await realpath(path);
  } catch (error) {
    // Workspace bins can point to build outputs that do not exist yet. Resolve
    // the existing ancestors, including macOS /var -> /private/var aliases.
    if (error.code !== "ENOENT" || dirname(path) === path) throw error;
    return join(await canonicalPath(dirname(path)), basename(path));
  }
}

/**
 * Publish only after acquiring Git's index lock and rechecking the original
 * bytes. The short synchronous section cannot be interrupted by a JS signal
 * handler halfway through. It keeps a rollback copy for ordinary I/O failures.
 * This is not a filesystem-wide transaction: SIGKILL/power loss can still need
 * the recovery copies written before entry. Avoid editing these files during
 * publication; compare-before-write is not an OS lock on editors.
 */
function publish(root, indexPath, originalIndex, candidateIndex, fixes) {
  const lockPath = `${indexPath}.lock`;
  const fd = openSync(lockPath, "wx");
  const written = [];
  let closed = false;
  let committed = false;
  try {
    if (!readFileSync(indexPath).equals(originalIndex))
      throw new Error(
        "the index changed during verification; retry the commit.",
      );
    for (const { path, before } of fixes) {
      if (!sameFile(regularFile(root, path), before))
        throw new Error(
          `${JSON.stringify(path)} changed during verification; retry the commit.`,
        );
    }
    for (const fix of fixes) {
      if (!sameFile(regularFile(root, fix.path), fix.before))
        throw new Error(
          `${JSON.stringify(fix.path)} changed during verification; retry the commit.`,
        );
      replaceFile(join(root, fix.path), fix.after);
      written.push(fix);
    }
    writeFileSync(fd, candidateIndex);
    closeSync(fd);
    closed = true;
    renameSync(lockPath, indexPath);
    committed = true;
  } catch (error) {
    for (const fix of written.reverse()) {
      try {
        if (!sameFile(regularFile(root, fix.path), fix.after))
          throw new Error("file changed after publication");
        replaceFile(join(root, fix.path), fix.before);
      } catch {
        error.recoveryNeeded = true;
      }
    }
    throw error;
  } finally {
    if (!closed) closeSync(fd);
    if (!committed) rmSync(lockPath, { force: true });
  }
}

function replaceFile(path, file) {
  const temporary = `${path}.ocm-fleet-commit-${randomUUID()}`;
  // Own the descriptor before writing so even a partial write failure removes
  // our temporary file. Never remove a pre-existing file when open fails.
  const fd = openSync(temporary, "wx", file.mode);
  let closed = false;
  let renamed = false;
  try {
    writeFileSync(fd, file.bytes);
    fchmodSync(fd, file.mode);
    closeSync(fd);
    closed = true;
    renameSync(temporary, path);
    renamed = true;
  } finally {
    if (!closed) closeSync(fd);
    if (!renamed) rmSync(temporary, { force: true });
  }
}

function regularFile(root, path) {
  // Never follow an unstaged replacement of a parent directory with a symlink.
  let current = root;
  for (const part of path.split("/")) {
    current = join(current, part);
    if (!existsSync(current) || lstatSync(current).isSymbolicLink())
      return null;
  }
  const stat = lstatSync(current);
  return stat.isFile()
    ? { bytes: readFileSync(current), mode: stat.mode & 0o777 }
    : null;
}

function sameFile(a, b, modeMask = 0o777) {
  return (
    !!a &&
    !!b &&
    ((a.mode ^ b.mode) & modeMask) === 0 &&
    a.bytes.equals(b.bytes)
  );
}

function changedSources(root, env) {
  return [
    ...new Set([
      ...paths(git(root, env, ["diff", "--name-only", "-z"])),
      ...paths(
        git(root, env, ["ls-files", "--others", "--exclude-standard", "-z"]),
      ),
    ]),
  ];
}

function paths(output) {
  return output.split("\0").filter(Boolean);
}

function git(cwd, env, args, { input, optional = false } = {}) {
  try {
    // Index filenames are data, including names that look like Git pathspecs.
    return execFileSync("git", ["--literal-pathspecs", ...args], {
      cwd,
      env,
      input,
      encoding: "utf8",
      maxBuffer: 64 * 1024 * 1024,
      stdio: ["pipe", "pipe", "pipe"],
    });
  } catch (error) {
    if (optional) return "";
    throw new Error(
      `git ${args[0]} failed: ${error.stderr?.toString().trim() || error.message}`,
      { cause: error },
    );
  }
}

if (
  process.argv[1] &&
  resolve(process.argv[1]) === fileURLToPath(import.meta.url)
) {
  const controller = new AbortController();
  const interrupt = () => controller.abort();
  process.on("SIGINT", interrupt);
  process.on("SIGTERM", interrupt);
  verifyCommit({ signal: controller.signal })
    .catch((error) => {
      console.error(`pre-commit: ${error.message}`);
      process.exitCode = 1;
    })
    .finally(() => {
      process.off("SIGINT", interrupt);
      process.off("SIGTERM", interrupt);
    });
}
