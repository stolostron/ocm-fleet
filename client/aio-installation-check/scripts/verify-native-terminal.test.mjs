import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { test } from "node:test";

import {
  createScreen,
  enclosingBox,
  repaintComplete,
  terminalEnvironment,
} from "./verify-native-terminal.mjs";

test("module import does not require the node-pty native addon", () => {
  // `npm ci --ignore-scripts` leaves node-pty's native addon unbuilt, so
  // importing this module for terminalEnvironment() must not load it. Block
  // its resolution in a fresh process and import the verifier like a test.
  const moduleUrl = JSON.stringify(
    new URL("./verify-native-terminal.mjs", import.meta.url).href,
  );
  const probe = `
    const { registerHooks } = await import("node:module");
    registerHooks({
      resolve(specifier, context, nextResolve) {
        if (specifier === "node-pty") {
          const error = new Error("node-pty native addon unavailable");
          error.code = "ERR_MODULE_NOT_FOUND";
          throw error;
        }
        return nextResolve(specifier, context);
      },
    });
    await import(${moduleUrl});
  `;
  const result = spawnSync(
    process.execPath,
    ["--input-type=module", "--eval", probe],
    { encoding: "utf8" },
  );
  assert.equal(result.status, 0, `${result.stderr}${result.stdout}`);
});

test("PTY color cases do not inherit NO_COLOR accidentally", () => {
  const inherited = { NO_COLOR: "1", TERM: "dumb", KEEP: "value" };
  const color = terminalEnvironment(false, inherited);
  const noColor = terminalEnvironment(true, inherited);
  assert.equal(color.NO_COLOR, undefined);
  assert.equal(noColor.NO_COLOR, "1");
  assert.equal(color.TERM, "xterm-256color");
  assert.equal(color.FORCE_COLOR, "1");
  assert.equal(color.KEEP, "value");
  assert.equal(inherited.NO_COLOR, "1");
});

test("verifier screen can read the buffer API from @xterm/headless 6", () => {
  const screen = createScreen(80, 24);
  try {
    assert.equal(typeof screen.buffer.active.getLine, "function");
    assert.equal(screen.buffer.active.baseY, 0);
  } finally {
    screen.dispose();
  }
});

test("arrow repaint only completes once a focus marker exists again", () => {
  const before = "│ ❯ Podman — recommended";
  // Ink redraws in chunks: mid-frame the marker row is erased or half
  // written, so the visible focus line disappears before the new one lands.
  assert.equal(repaintComplete(before, undefined), false);
  assert.equal(repaintComplete(before, null), false);
  assert.equal(repaintComplete(before, before), false);
  assert.equal(repaintComplete(before, "│ ❯ Docker — limited"), true);
  assert.equal(repaintComplete(undefined, "│ ❯ Docker — limited"), true);
});

test("menu assertions target the box containing the focus marker", () => {
  // The banner box renders above the active menu; picking the first box on
  // the screen would measure the banner instead of the menu.
  const lines = [
    "╭──────────╮",
    "│ OCM Fleet│",
    "╰──────────╯",
    "",
    "Choose container runtime:",
    "╭──────────╮",
    "│  Podman  │",
    "│ ❯ Docker │",
    "╰──────────╯",
  ];
  const box = enclosingBox(lines, 7);
  assert.deepEqual(box, { top: 5, bottom: 8, right: 11 });
  assert.equal(box.top, 5);
  assert.equal(
    lines.slice(box.top, box.bottom).filter((line) => line.includes("❯"))
      .length,
    1,
  );
  // A row inside the banner box resolves to the banner box, not the menu.
  assert.deepEqual(enclosingBox(lines, 1), { top: 0, bottom: 2, right: 11 });
  // A row outside any box has no enclosing border.
  assert.equal(enclosingBox(lines, 4), undefined);
});
