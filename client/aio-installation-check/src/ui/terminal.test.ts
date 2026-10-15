/* eslint playwright/no-standalone-expect: off -- Vitest assertions run inside this test callback. */
import stringWidth from "string-width";
import { expect, it, vi } from "vitest";

import { TerminalCancelled } from "./keyboard-selection";
import { createTerminal } from "./terminal";

it("renders terminal lines through Ink without alternate-screen controls", () => {
  const writeSpy = vi.spyOn(process.stdout, "write");
  writeSpy.mockImplementation(() => true);

  try {
    createTerminal().write("OME FleetShift AIO check");

    const output = writeSpy.mock.calls.map(([chunk]) => String(chunk)).join("");
    expect(output).toBe("OME FleetShift AIO check\n");
    expect(output).not.toMatch(/\u001b\[\?1049[hl]/);
  } finally {
    writeSpy.mockRestore();
  }
});

function terminalHarness(
  tty: boolean,
  initialRaw: boolean | undefined = false,
  columns = 32,
) {
  const restore: (() => void)[] = [];
  function property(object: object, name: string, value: unknown): void {
    const descriptor = Object.getOwnPropertyDescriptor(object, name);
    Object.defineProperty(object, name, {
      configurable: true,
      writable: true,
      value,
    });
    restore.push(() => {
      if (descriptor) Object.defineProperty(object, name, descriptor);
      else Reflect.deleteProperty(object, name);
    });
  }
  property(process.stdin, "isTTY", tty);
  property(process.stdout, "isTTY", tty);
  property(process.stdout, "columns", columns);
  property(process.stdout, "rows", 12);
  property(process.stdin, "isRaw", initialRaw);
  const rawMode = vi.fn((enabled: boolean) => {
    property(process.stdin, "isRaw", enabled);
    return process.stdin;
  });
  property(process.stdin, "setRawMode", rawMode);
  const write = vi
    .spyOn(process.stdout, "write")
    .mockImplementation(() => true);
  const resume = vi
    .spyOn(process.stdin, "resume")
    .mockReturnValue(process.stdin);
  const pause = vi.spyOn(process.stdin, "pause").mockReturnValue(process.stdin);
  return {
    rawMode,
    resize: (width: number) => {
      property(process.stdout, "columns", width);
      process.stdout.emit("resize");
    },
    clear: () => write.mockClear(),
    output: () => write.mock.calls.map(([chunk]) => String(chunk)).join(""),
    restore: () => {
      write.mockRestore();
      resume.mockRestore();
      pause.mockRestore();
      for (const undo of restore.reverse()) undo();
    },
  };
}

it.each([48, 80, 160, 240])(
  "wraps command output at responsive width %i instead of 76",
  (columns) => {
    const harness = terminalHarness(true, false, columns);
    try {
      const command = "podman run " + "x".repeat(120);
      createTerminal().write(command);
      const lines = harness.output().trim().split("\r\n");
      expect(lines.join("")).toBe(command);
      expect(lines.every((line) => stringWidth(line) < columns)).toBe(true);
      expect(lines.length).toBe(columns >= 160 ? 1 : columns === 80 ? 2 : 3);
    } finally {
      harness.restore();
    }
  },
);

it("resizes bounded menu frames down and up while preserving focus and raw cleanup", async () => {
  const harness = terminalHarness(true, false, 160);
  const label = "Long launch choice " + "x".repeat(110);
  let choice: Promise<number> | undefined;
  try {
    choice = createTerminal().choose("Choose", [label, "No"]);
    await mounted();
    for (const columns of [160, 48, 80, 240, 48, 160]) {
      harness.clear();
      harness.resize(columns);
      const frame = harness.output().replace(/\u001b\[[0-?]*[ -/]*[@-~]/g, "");
      const lines = frame.split("\r\n").filter(Boolean);
      expect(lines.every((line) => stringWidth(line) < columns)).toBe(true);
      expect(lines[0].replace(/\r/g, "").length).toBe(columns - 1);
      expect(lines.length).toBeLessThan(12);
      expect(frame).toContain("❯ Long launch choice");
      if (columns >= 160) expect(frame).toContain(label);
      else expect(frame).not.toContain(label);
      process.stdin.emit("data", "\u001b[B\u001b[A");
    }
    process.stdin.emit("data", "\r");
    expect(await choice).toBe(0);
    expect(harness.rawMode.mock.calls).toEqual([[true], [false]]);
  } finally {
    process.stdin.emit("data", "\u0003");
    await choice?.catch(() => {});
    harness.restore();
  }
});

it("uses numeric selection when physical width cannot fit a bordered menu", async () => {
  const harness = terminalHarness(true, false, 7);
  try {
    const choice = createTerminal().choose("Choose", ["Run", "No"]);
    process.stdin.emit("data", "2\r");
    expect(await choice).toBe(1);
    expect(harness.output()).not.toContain("╭");
    expect(harness.output()).not.toContain("\u001b[?25l");
    expect(harness.rawMode.mock.calls).toEqual([[true], [false]]);
  } finally {
    harness.restore();
  }
});

async function mounted(): Promise<void> {
  await new Promise<void>((resolve) => setImmediate(resolve));
}

it("arrows update visible focus, wrap, and Enter confirms without leaked raw mode", async () => {
  const harness = terminalHarness(true);
  const listeners = process.stdin.listenerCount("data");
  try {
    const choice = createTerminal().choose(
      "Choose",
      ["First", "Second", "Third"],
      1,
    );
    await mounted();
    expect(harness.output()).toContain("❯ Second");
    process.stdin.emit("data", "\u001b[A");
    expect(harness.output()).toContain("❯ First");
    process.stdin.emit("data", "\u001b[A\r");
    expect(await choice).toBe(2);
    expect(harness.output()).toContain("❯ Third");
    expect(harness.rawMode.mock.calls).toEqual([[true], [false]]);
    expect(harness.output()).toContain("\u001b[?25h");
    expect(harness.output()).not.toMatch(
      /\u001b\[\?(?:1049|1047|47)[hl]|\u001b\[3J/,
    );
    expect(process.stdin.listenerCount("data")).toBe(listeners);
  } finally {
    harness.restore();
  }
});

it("Ctrl-C rejects selection and restores cursor, raw mode, and signal listeners", async () => {
  const harness = terminalHarness(true);
  const signals = process.listenerCount("SIGINT");
  try {
    const choice = createTerminal().choose("Choose", ["Run", "No"]);
    const rejected = expect(choice).rejects.toBeInstanceOf(TerminalCancelled);
    await mounted();
    process.stdin.emit("data", "\u0003");
    await rejected;
    expect(harness.rawMode.mock.calls).toEqual([[true], [false]]);
    expect(harness.output()).toContain("\u001b[?25h");
    expect(process.listenerCount("SIGINT")).toBe(signals);
  } finally {
    harness.restore();
  }
});

it("non-TTY accepts buffered numeric lines, retries invalid input, and emits no ANSI", async () => {
  const harness = terminalHarness(false);
  try {
    const choice = createTerminal().choose("Choose", ["Run", "No"]);
    process.stdin.emit("data", "invalid\n2\n");
    expect(await choice).toBe(1);
    expect(harness.output()).toContain("2) No");
    expect(harness.output()).toContain("Enter a number from 1 to 2.");
    expect(harness.output()).not.toContain("\u001b");
    expect(harness.rawMode).not.toHaveBeenCalled();
  } finally {
    harness.restore();
  }
});

it("numeric Enter accepts explicit default and empty choices reject before reading", async () => {
  const harness = terminalHarness(false);
  try {
    const terminal = createTerminal();
    await expect(terminal.choose("Empty", [])).rejects.toThrow("empty list");
    const choice = terminal.choose("Choose", ["Run", "No"], 1);
    process.stdin.emit("data", "\n");
    expect(await choice).toBe(1);
  } finally {
    harness.restore();
  }
});

it("EOF fails without spinning on an unavailable confirmation", async () => {
  const harness = terminalHarness(false);
  try {
    const choice = createTerminal().choose("Choose", ["Run", "No"]);
    const rejected = expect(choice).rejects.toThrow("Input closed");
    process.stdin.emit("end");
    await rejected;
  } finally {
    harness.restore();
  }
});

it.each(["\r", "\n", "\r\n"])(
  "text input submits %j and restores TTY state",
  async (enter) => {
    const harness = terminalHarness(true);
    const listeners = process.stdin.listenerCount("data");
    try {
      const answer = createTerminal().input("Path: ");
      process.stdin.emit("data", "/tmp/existing" + enter);
      expect(await answer).toBe("/tmp/existing");
      expect(harness.rawMode.mock.calls).toEqual([[true], [false]]);
      expect(process.stdin.listenerCount("data")).toBe(listeners);
    } finally {
      harness.restore();
    }
  },
);

it("split CRLF does not submit next prompt, paste and backspace preserve Unicode", async () => {
  const harness = terminalHarness(true);
  try {
    const terminal = createTerminal();
    const first = terminal.input("First: ");
    process.stdin.emit("data", "hello\r");
    expect(await first).toBe("hello");
    const next = terminal.input("Next: ");
    process.stdin.emit("data", "\n\u001b[200~/tmp/🦕x\u007f\b🦖\u001b[201~\r");
    expect(await next).toBe("/tmp/🦖");
  } finally {
    harness.restore();
  }
});

it.each(["data", "SIGINT"])(
  "text CtrlC via %s cancels and cleans up",
  async (event) => {
    const harness = terminalHarness(true);
    const dataListeners = process.stdin.listenerCount("data");
    const signals = process.listenerCount("SIGINT");
    try {
      const answer = createTerminal().input("Path: ");
      const rejected = expect(answer).rejects.toBeInstanceOf(TerminalCancelled);
      if (event === "data") process.stdin.emit("data", "partial\u0003");
      else process.emit("SIGINT");
      await rejected;
      expect(harness.rawMode.mock.calls).toEqual([[true], [false]]);
      expect(harness.output()).toContain("\u001b[?25h");
      expect(process.stdin.listenerCount("data")).toBe(dataListeners);
      expect(process.listenerCount("SIGINT")).toBe(signals);
    } finally {
      harness.restore();
    }
  },
);

it("EOF state belongs to terminal instance; fresh input still accepts buffered CRLF lines", async () => {
  const harness = terminalHarness(false);
  try {
    const terminal = createTerminal();
    const closed = terminal.input("Closed: ");
    const rejected = expect(closed).rejects.toThrow("Input closed");
    process.stdin.emit("end");
    await rejected;
    const fresh = createTerminal();
    const answer = fresh.input("Fresh: ");
    process.stdin.emit("data", "one\r\ntwo\n");
    expect(await answer).toBe("one");
    expect(await fresh.input("Buffered: ")).toBe("two");
    expect(harness.rawMode).not.toHaveBeenCalled();
  } finally {
    harness.restore();
  }
});

it.each([true, null])(
  "restores existing raw state %j for menu and text prompts",
  async (initial) => {
    const harness = terminalHarness(true, initial === true);
    if (initial === null) Reflect.deleteProperty(process.stdin, "isRaw");
    try {
      const terminal = createTerminal();
      const menu = terminal.choose("Choose", ["One", "Two"]);
      await mounted();
      process.stdin.emit("data", "\r");
      expect(await menu).toBe(0);
      const answer = terminal.input("Path: ");
      process.stdin.emit("data", "\n/tmp\r");
      expect(await answer).toBe("/tmp");
      expect(harness.rawMode.mock.calls).toEqual([
        [true],
        [initial === true],
        [true],
        [initial === true],
      ]);
    } finally {
      harness.restore();
    }
  },
);

it("text EOF flushes split UTF-8 and restores mode; next EOF rejects", async () => {
  const harness = terminalHarness(true);
  try {
    const terminal = createTerminal();
    const answer = terminal.input("Path: ");
    const bytes = new TextEncoder().encode("/tmp/🦕");
    process.stdin.emit("data", bytes.slice(0, -1));
    process.stdin.emit("data", bytes.slice(-1));
    process.stdin.emit("end");
    expect(await answer).toBe("/tmp/🦕");
    await expect(terminal.input("Closed: ")).rejects.toThrow("Input closed");
    expect(harness.rawMode.mock.calls).toEqual([
      [true],
      [false],
      [true],
      [false],
    ]);
  } finally {
    harness.restore();
  }
});

it("text cancellation releases native wake timer and all prompt listeners", async () => {
  const harness = terminalHarness(true);
  const native = Object.getOwnPropertyDescriptor(process.versions, "perry");
  vi.useFakeTimers();
  try {
    Object.defineProperty(process.versions, "perry", {
      configurable: true,
      value: "0.5.1520",
    });
    const timers = vi.getTimerCount();
    const terminal = createTerminal();
    const answer = terminal.input("Path: ");
    const rejected = expect(answer).rejects.toBeInstanceOf(TerminalCancelled);
    expect(vi.getTimerCount()).toBeGreaterThan(timers);
    process.stdin.emit("data", "\u0003");
    await rejected;
    expect(vi.getTimerCount()).toBe(timers);
  } finally {
    if (native) Object.defineProperty(process.versions, "perry", native);
    else Reflect.deleteProperty(process.versions, "perry");
    vi.useRealTimers();
    harness.restore();
  }
});

it("failed raw acquisition rejects after restoring mode and prompt listeners", async () => {
  const harness = terminalHarness(true);
  const signals = process.listenerCount("SIGINT");
  const listeners = process.stdin.listenerCount("data");
  try {
    harness.rawMode.mockImplementationOnce(() => {
      throw new Error("Raw mode unavailable");
    });
    await expect(createTerminal().input("Path: ")).rejects.toThrow(
      "Raw mode unavailable",
    );
    expect(harness.rawMode.mock.calls).toEqual([[true], [false]]);
    expect(harness.output()).toContain("\u001b[?25h");
    expect(process.listenerCount("SIGINT")).toBe(signals);
    expect(process.stdin.listenerCount("data")).toBe(listeners);
  } finally {
    harness.restore();
  }
});

it("native dispatch snapshots keep later pipe chunks buffered after prompt settles", async () => {
  const harness = terminalHarness(false);
  try {
    const terminal = createTerminal();
    const choice = terminal.choose("Choose", ["Run", "No"]);
    // Perry snapshots callbacks once for an entire dispatch batch. Removal and
    // pause during the first callback cannot prevent its remaining invocations.
    const callback = process.stdin.listeners("data").at(-1) as (
      chunk: string,
    ) => void;
    callback("invalid\r\n");
    callback("2\r\n");
    expect(await choice).toBe(1);
    expect(harness.output()).toContain("Enter a number from 1 to 2.");
  } finally {
    harness.restore();
  }
});
