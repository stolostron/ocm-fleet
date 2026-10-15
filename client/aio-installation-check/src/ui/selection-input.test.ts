/* eslint-disable playwright/no-standalone-expect */
import { expect, it } from "vitest";

import { SelectionInput } from "./selection-input";

it("handles split arrows, application arrows, rapid arrow+Enter and Ctrl-C", () => {
  const parser = new SelectionInput();
  expect(parser.push("\u001b")).toEqual([]);
  expect(parser.push("[")).toEqual([]);
  expect(parser.push("B\u001bOA\r")).toEqual(["down", "up", "confirm"]);
  expect(parser.push("\u0003")).toEqual(["cancel"]);
});

it("ignores unrelated control sequences and typed labels", () => {
  const parser = new SelectionInput();
  expect(parser.push("no\u001b[1;5C\u001b[3~")).toEqual([]);
  expect(parser.push("\u001b[B\n")).toEqual(["down", "confirm"]);
});
