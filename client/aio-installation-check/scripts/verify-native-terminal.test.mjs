import assert from "node:assert/strict";
import { test } from "node:test";

import { terminalEnvironment } from "./verify-native-terminal.mjs";

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
