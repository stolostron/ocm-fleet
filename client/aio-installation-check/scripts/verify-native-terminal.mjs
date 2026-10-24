#!/usr/bin/env node
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdir, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { Terminal } from "@xterm/headless";
import pty from "node-pty";
import { $ } from "zx";

$.verbose = false;

const projectRoot = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "..",
);
const binary = path.join(projectRoot, "dist/native/aio-installation-check");
const output = path.join(projectRoot, "dist/native-terminal-evidence");
const sleep = (milliseconds) =>
  new Promise((resolve) => setTimeout(resolve, milliseconds));
const stripAnsi = (text) => text.replace(/\u001b\[[0-?]*[ -/]*[@-~]/g, "");

class TerminalSession {
  constructor(name, columns, rows, noColor = false) {
    this.name = name;
    this.columns = columns;
    this.rows = rows;
    this.raw = "";
    this.latencies = [];
    this.pendingWrite = Promise.resolve();
    this.exitEvent = undefined;
    this.screen = new Terminal({ cols: columns, rows, scrollback: 10_000 });
    this.process = pty.spawn(binary, ["--with-kind"], {
      name: "xterm-256color",
      cols: columns,
      rows,
      cwd: projectRoot,
      env: {
        ...process.env,
        TERM: "xterm-256color",
        FORCE_COLOR: "1",
        ...(noColor ? { NO_COLOR: "1" } : {}),
      },
    });
    this.exit = new Promise((resolve) => {
      this.process.onExit((event) => {
        this.exitEvent = event;
        resolve(event);
      });
    });
    this.process.onData((data) => {
      this.raw += data;
      this.pendingWrite = this.pendingWrite.then(
        () => new Promise((resolve) => this.screen.write(data, resolve)),
      );
    });
  }

  async flush() {
    await this.pendingWrite;
  }

  visible() {
    const buffer = this.screen.buffer.active;
    return Array.from(
      { length: this.rows },
      (_, row) =>
        buffer.getLine(buffer.baseY + row)?.translateToString(true) ?? "",
    ).join("\n");
  }

  async waitFor(text, timeoutMs = 25_000) {
    const deadline = Date.now() + timeoutMs;
    while (true) {
      await this.flush();
      if (this.visible().includes(text)) return;
      assert.equal(
        this.exitEvent,
        undefined,
        `${this.name}: exited before ${JSON.stringify(text)}\n${this.visible()}`,
      );
      assert.ok(
        Date.now() < deadline,
        `${this.name}: timed out waiting for ${JSON.stringify(text)}\n${this.visible()}`,
      );
      await sleep(10);
    }
  }

  focus() {
    return this.visible()
      .split("\n")
      .findLast((line) => line.includes("❯"));
  }

  async move(key) {
    const before = this.focus();
    const oldBaseY = this.screen.buffer.active.baseY;
    const started = Date.now();
    this.process.write(key);
    while (this.focus() === before) {
      assert.equal(
        this.exitEvent,
        undefined,
        `${this.name}: exited on arrow input`,
      );
      assert.ok(
        Date.now() - started < 2_000,
        `${this.name}: arrow failed to repaint`,
      );
      await this.flush();
      await sleep(2);
    }
    this.latencies.push(Date.now() - started);
    await this.flush();
    assert.equal(
      this.screen.buffer.active.baseY,
      oldBaseY,
      "arrow appended a history frame",
    );
    assert.equal(
      this.screen.buffer.active.cursorX,
      0,
      "redraw did not return cursor to column zero",
    );

    const lines = this.visible().split("\n");
    const top = lines.findIndex((line) => line.startsWith("╭"));
    assert.notEqual(top, -1, "active menu has no top border");
    const bottom = lines.findIndex(
      (line, index) => index > top && line.startsWith("╰"),
    );
    assert.notEqual(bottom, -1, "active menu has no bottom border");
    const right = [...lines[top]].indexOf("╮");
    for (const row of lines.slice(top + 1, bottom)) {
      assert.equal(row[0], "│", `misaligned left border: ${row}`);
      assert.equal([...row][right], "│", `misaligned right border: ${row}`);
    }
    assert.equal(
      lines.slice(top, bottom).filter((line) => line.includes("❯")).length,
      1,
      "active menu must show exactly one focus marker",
    );
  }

  async exercise(label) {
    await this.idleWait();
    const before = this.visible();
    const oldBaseY = this.screen.buffer.active.baseY;
    for (let count = 0; count < 10; count += 1) {
      await this.move("\u001b[B");
      await this.move("\u001b[A");
      await this.move("\u001b[B");
      await this.move("\u001b[A");
    }
    assert.equal(
      this.visible(),
      before,
      "paired arrows did not restore exact screen contents",
    );
    assert.equal(
      this.screen.buffer.active.baseY,
      oldBaseY,
      "menu redraw changed scrollback",
    );
    await this.idleWait();
    await this.capture(label);
  }

  async idleWait(milliseconds = 1_000) {
    const before = this.visible();
    await sleep(milliseconds);
    await this.flush();
    assert.equal(
      this.visible(),
      before,
      "idle menu repainted or appended output",
    );
    assert.equal(
      this.exitEvent,
      undefined,
      "interactive prompt exited while idle",
    );
  }

  async choose(down = 0) {
    for (let count = 0; count < down; count += 1) await this.move("\u001b[B");
    this.process.write("\r");
  }

  async capture(label) {
    await this.flush();
    await writeFile(
      path.join(output, `${this.name}-${label}.txt`),
      `${this.visible()}\n`,
    );
    await writeFile(path.join(output, `${this.name}-${label}.ansi`), this.raw);
  }

  async finish(exitCode) {
    const event = await new Promise((resolve, reject) => {
      const timeout = setTimeout(
        () => reject(new Error(`${this.name}: process did not exit`)),
        10_000,
      );
      this.exit.then((result) => {
        clearTimeout(timeout);
        resolve(result);
      });
    });
    await this.flush();
    assert.equal(
      event.exitCode,
      exitCode,
      `${this.name}: unexpected process exit`,
    );
    const hiddenCursor = this.raw.lastIndexOf("\u001b[?25l");
    const shownCursor = this.raw.lastIndexOf("\u001b[?25h");
    assert.ok(shownCursor > hiddenCursor, "cursor was not restored");
    assert.ok(
      !/[\u001b]\[\?(?:1049|1047|47)[hl]/.test(this.raw),
      "alternate screen was used",
    );
    assert.ok(
      !this.raw.includes("\u001b[2J") && !this.raw.includes("\u001b[3J"),
      "scrollback was cleared",
    );
    assert.ok(
      !this.raw.includes('unique "key"'),
      "React emitted duplicate-key warning",
    );
    assert.ok(
      !this.raw.includes("(optional)"),
      "passing prerequisite mislabeled optional",
    );
    await this.capture("final");
    return event;
  }

  async close() {
    if (this.exitEvent === undefined) {
      this.process.kill();
      await Promise.race([this.exit, sleep(2_000)]);
    }
    this.screen.dispose();
  }
}

async function fullFlow(columns, rows, { noColor = false } = {}) {
  const session = new TerminalSession(
    `native-${columns}x${rows}${noColor ? "-no-color" : ""}`,
    columns,
    rows,
    noColor,
  );
  try {
    await session.waitFor("Choose container runtime");
    await session.waitFor("❯ Podman");
    await session.exercise("runtime");
    await session.choose();

    await session.waitFor("Set up AIO launch?");
    await session.waitFor("❯ Set up launch");
    await session.exercise("setup");
    await session.choose();

    await session.waitFor("Optional: persist");
    await session.waitFor("❯ Persist data");
    await session.exercise("persistence");
    await session.choose(1);

    await session.waitFor("Run this AIO launch command?");
    await session.waitFor("❯ Run");
    assert.ok(
      session.raw.includes("--pull=always"),
      "launch preview missing --pull=always",
    );
    assert.ok(
      !session.raw.includes(":/data"),
      "Skip persistence still mounted /data",
    );
    await session.exercise("preview");
    await session.choose(1);

    await session.waitFor("Copy this exact command");
    await session.waitFor("❯ Copy");
    await session.exercise("copy");
    await session.choose(1);

    await session.finish(0);
    const maximumArrowMs = Math.max(...session.latencies);
    assert.ok(
      maximumArrowMs < 100,
      `arrow redraw exceeded 100 ms (${maximumArrowMs} ms)`,
    );
    if (noColor) {
      assert.ok(
        !/\u001b\[[0-9;]*m/.test(session.raw),
        "NO_COLOR emitted ANSI color",
      );
    } else {
      assert.ok(
        session.raw.includes("\u001b[36m"),
        "expected Ink color output",
      );
    }
    return {
      screen: session.name,
      arrows: session.latencies.length,
      maximumArrowMs,
      exit: 0,
    };
  } finally {
    await session.close();
  }
}

async function cancelFlow(signal = false) {
  const name = signal ? "native-sigterm" : "native-ctrl-c";
  const session = new TerminalSession(name, 80, 24);
  try {
    await session.waitFor("❯ Podman");
    await session.choose();
    await session.waitFor("❯ Set up launch");
    await session.idleWait();
    if (signal) session.process.kill("SIGTERM");
    else session.process.write("\u0003");
    const exit = await session.finish(130);
    assert.equal(exit.exitCode, 130);
    assert.ok(stripAnsi(session.raw).includes("Cancelled."));
    return { screen: name, exit: 130, cursorRestored: true };
  } finally {
    await session.close();
  }
}

function pipeFlows() {
  const environment = { ...process.env, FORCE_COLOR: "1" };
  delete environment.NO_COLOR;
  const full = spawnSync(binary, ["--with-kind"], {
    input: "1\n1\n2\n2\n2\n",
    encoding: "utf8",
    env: environment,
    timeout: 30_000,
  });
  assert.equal(full.status, 0, `${full.stdout}\n${full.stderr}`);
  assert.ok(
    full.stdout.includes("Launch declined"),
    "numeric flow did not finish",
  );
  assert.ok(!/\u001b/.test(full.stdout), "non-TTY output emitted ANSI");

  const eof = spawnSync(binary, ["--with-kind"], {
    input: "",
    encoding: "utf8",
    timeout: 30_000,
  });
  assert.equal(eof.status, 1);
  assert.ok(eof.stderr.includes("Input closed"));
  return { numeric: "full decline flow passed", EOF: "exit 1, explicit error" };
}

async function podmanInventory() {
  const result = await $`podman ps -a --format '{{.ID}}'`.quiet();
  return result.stdout.trim();
}

await mkdir(output, { recursive: true });
const before = await podmanInventory();
const results = [
  await fullFlow(80, 24),
  await fullFlow(48, 18),
  await fullFlow(80, 24, { noColor: true }),
  await cancelFlow(),
  await cancelFlow(true),
  pipeFlows(),
];
assert.equal(
  await podmanInventory(),
  before,
  "container inventory changed during verification",
);
results.push({ containerInventoryUnchanged: true });
await writeFile(
  path.join(output, "results.json"),
  `${JSON.stringify(results, null, 2)}\n`,
);
console.log(JSON.stringify(results, null, 2));
console.log(`Evidence: ${output}`);
