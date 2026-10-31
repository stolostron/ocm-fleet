/* eslint-disable playwright/no-standalone-expect */
import { describe, expect, it } from "vitest";

import {
  InlineRegion,
  terminalColumns,
  terminalNewlines,
} from "./inline-region";

describe("raw-mode inline transport", () => {
  it.each([48, 80, 160, 240])(
    "uses all but the last physical column at %i columns",
    (columns) => {
      expect(terminalColumns(columns)).toBe(columns - 1);
      expect(
        new InlineRegion().draw(
          "x".repeat(terminalColumns(columns)),
          columns,
          24,
        ),
      ).toBe("\r" + "x".repeat(columns - 1) + "\r\n");
    },
  );

  it.each([
    undefined,
    NaN,
    Infinity,
    -Infinity,
    0,
    -1,
    null as unknown as number,
    "wide" as unknown as number,
  ])("falls back to an 80-column terminal for invalid size %j", (columns) => {
    expect(terminalColumns(columns)).toBe(79);
  });

  it.each([1, 2, 3, 7, 8, 48.9])(
    "does not inflate narrow or fractional physical width %j",
    (columns) => {
      expect(terminalColumns(columns)).toBe(
        Math.max(1, Math.floor(columns) - 1),
      );
      expect(terminalColumns(columns)).toBeLessThanOrEqual(Math.floor(columns));
    },
  );

  it("returns column zero on every newline with OPOST disabled", () => {
    expect(terminalNewlines("first\nsecond\r\nthird")).toBe(
      "first\r\nsecond\r\nthird",
    );
    expect(new InlineRegion().draw("one\ntwo", 48, 18)).toBe(
      "\rone\r\ntwo\r\n",
    );
  });

  it("erases physical rows after resize, ignoring ANSI style bytes", () => {
    const region = new InlineRegion();
    region.draw("\u001b[36m" + "x".repeat(75) + "\u001b[0m\nshort", 80, 24);
    expect(region.draw("new", 48, 18)).toBe(
      "\u001b[1A\r\u001b[2K".repeat(3) + "\rnew\r\n",
    );
  });

  it("rejects last-column autowrap and viewport overflow", () => {
    expect(terminalColumns(48)).toBe(47);
    expect(() => new InlineRegion().draw("x".repeat(48), 48, 18)).toThrow(
      "physical",
    );
    expect(() => new InlineRegion().draw("a\n".repeat(17), 48, 18)).toThrow(
      "physical",
    );
  });

  it("bounds resized wide frames and erases reflowed rows on shrink and growth", () => {
    const region = new InlineRegion();
    region.draw("x".repeat(159) + "\nshort", 160, 24);
    expect(region.draw("x".repeat(47), 48, 24)).toBe(
      "\u001b[1A\r\u001b[2K".repeat(5) + "\r" + "x".repeat(47) + "\r\n",
    );
    expect(region.draw("x".repeat(239), 240, 24)).toBe(
      "\u001b[1A\r\u001b[2K\r" + "x".repeat(239) + "\r\n",
    );
    expect(() => region.draw("x".repeat(48), 48, 24)).toThrow("physical");
  });
});
