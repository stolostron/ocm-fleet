// Perry needs this explicit import to lower JSX as React elements, not eager calls.
import "react/jsx-runtime";

import { spawn } from "node:child_process";
import { StringDecoder } from "node:string_decoder";

import { renderToString } from "ink";

import type { CheckResult } from "../contracts";
import { terminalColumns, terminalNewlines } from "./inline-region";
import { inputWake } from "./input-wake";
import { chooseInkKeyboard, TerminalCancelled } from "./keyboard-selection";
import { LineInput } from "./line-input";
import { CheckPanel, Message, type MessageStyle } from "./presentation";
import { acquireRawMode } from "./raw-mode";

export interface Terminal {
  platform: string;
  write(message: string, style?: MessageStyle): void;
  result?(result: CheckResult): void;
  input(prompt: string): Promise<string>;
  choose(
    prompt: string,
    choices: string[],
    defaultIndex?: number,
  ): Promise<number>;
  wait(milliseconds: number): Promise<void>;
  runCommand(command: string): Promise<boolean>;
  copy(text: string): Promise<boolean>;
}

function columns(): number {
  return terminalColumns(process.stdout.columns);
}

function hasColor(): boolean {
  return (
    Boolean(process.stdout.isTTY) &&
    process.env.NO_COLOR === undefined &&
    process.env.TERM !== "dumb"
  );
}

function renderInkText(message: string, style?: MessageStyle): string {
  return renderToString(
    <Message message={message} style={style} color={hasColor()} />,
    { columns: columns() },
  );
}

function writeInkLine(message: string, style?: MessageStyle): void {
  const text = `${renderInkText(message, style)}\n`;
  process.stdout.write(process.stdout.isTTY ? terminalNewlines(text) : text);
}

function writeInkPrompt(message: string): void {
  process.stdout.write(renderInkText(message));
}

function readLine(
  prompt: string,
  input: LineInput,
  decoder: StringDecoder,
): Promise<string> {
  return new Promise((resolve, reject) => {
    const stopWake = inputWake();
    const raw =
      Boolean(process.stdin.isTTY) &&
      typeof process.stdin.setRawMode === "function";
    let restoreRaw = (): void => {};
    let settled = false;
    let renderedRows = 0;
    const draw = (text = input.text): void => {
      // Ink handles wrapping; transport erases only this prompt's visible rows.
      const lines = renderInkText(prompt + text).split("\n");
      const frame = lines
        .slice(-Math.max(1, (process.stdout.rows || 24) - 1))
        .join("\n");
      let erase = renderedRows > 0 ? "\r\u001b[2K" : "";
      for (let row = 1; row < renderedRows; row += 1)
        erase += "\u001b[1A\r\u001b[2K";
      process.stdout.write(erase + terminalNewlines(frame));
      renderedRows = frame.split("\n").length;
    };
    const cleanup = (): void => {
      stopWake();
      process.stdin.removeListener("data", onData);
      process.stdin.removeListener("end", onEnd);
      process.stdout.removeListener("resize", resized);
      process.removeListener("SIGINT", cancel);
      process.removeListener("SIGTERM", cancel);
      try {
        restoreRaw();
      } finally {
        process.stdin.pause();
        if (raw) process.stdout.write("\u001b[?25h\r\n");
      }
    };
    const finish = (line?: string, error?: unknown): void => {
      if (settled) return;
      settled = true;
      try {
        cleanup();
      } catch (restoreError) {
        error ??= restoreError;
      }
      if (error !== undefined) reject(error);
      else resolve(line!);
    };
    const cancel = (): void => finish(undefined, new TerminalCancelled());
    const consume = (): void => {
      const line = input.take(raw);
      if (line !== undefined) {
        if (raw) draw(line);
        finish(line);
      } else if (raw) draw();
    };
    const resized = (): void => {
      try {
        draw();
      } catch (error) {
        finish(undefined, error);
      }
    };
    const onData = (chunk: Uint8Array | string): void => {
      try {
        input.push(typeof chunk === "string" ? chunk : decoder.write(chunk));
        // Perry dispatches a snapshot of listeners for the entire queued batch.
        // Keep later bytes for the next prompt even after this listener was removed.
        if (!settled) consume();
      } catch (error) {
        finish(undefined, error);
      }
    };
    const onEnd = (): void => {
      input.ended = true;
      try {
        input.push(decoder.end());
        if (!settled) consume();
      } catch (error) {
        finish(undefined, error);
      }
    };
    try {
      if (raw) {
        restoreRaw = acquireRawMode();
        process.stdout.write("\u001b[?25h");
        draw();
        process.stdout.on("resize", resized);
      } else writeInkPrompt(prompt);
      process.on("SIGINT", cancel);
      process.on("SIGTERM", cancel);
      process.stdin.on("data", onData);
      process.stdin.on("end", onEnd);
      if (process.stdin.readableEnded) input.ended = true;
      consume();
      if (!settled) process.stdin.resume();
    } catch (error) {
      finish(undefined, error);
    }
  });
}

function waitForCommand(child: ReturnType<typeof spawn>): Promise<boolean> {
  return new Promise((resolve) => {
    child.on("error", () => resolve(false));
    child.on("close", (code) => resolve(code === 0));
  });
}

function spawnCommand(executable: string, args: string[]): Promise<boolean> {
  const child = spawn(executable, args, { stdio: "inherit" });
  return waitForCommand(child);
}

function copyCommand(
  executable: string,
  args: string[],
  text: string,
): Promise<boolean> {
  const child = spawn(executable, args, {
    stdio: ["pipe", "ignore", "ignore"],
  });
  const finished = waitForCommand(child);
  child.stdin.write(text);
  child.stdin.end();
  return finished;
}

async function copyToClipboard(text: string): Promise<boolean> {
  const candidates: [string, string[]][] =
    process.platform === "darwin"
      ? [["pbcopy", []]]
      : [
          ["wl-copy", []],
          ["xclip", ["-selection", "clipboard"]],
          ["xsel", ["--clipboard", "--input"]],
        ];

  for (const [executable, args] of candidates) {
    if (await copyCommand(executable, args, text)) return true;
  }
  return false;
}

export function createTerminal(): Terminal {
  const input = new LineInput();
  // Perry's TextDecoder ignores { stream: true }; StringDecoder retains UTF-8
  // fragments from its byte-at-a-time raw reader, just as Node does.
  const decoder = new StringDecoder("utf8");
  return {
    platform: process.platform,
    write(message, style) {
      writeInkLine(message, style);
    },
    result(result) {
      const text = `${renderToString(<CheckPanel result={result} color={hasColor()} />, { columns: columns() })}\n`;
      process.stdout.write(
        process.stdout.isTTY ? terminalNewlines(text) : text,
      );
    },
    input(prompt) {
      return readLine(prompt, input, decoder);
    },
    async choose(prompt, choices, defaultIndex) {
      if (choices.length === 0)
        throw new Error("Cannot choose from an empty list.");
      const defaultChoice =
        defaultIndex !== undefined &&
        Number.isInteger(defaultIndex) &&
        defaultIndex >= 0 &&
        defaultIndex < choices.length
          ? defaultIndex
          : 0;
      writeInkLine(prompt, "heading");
      const interactive =
        process.stdin.isTTY &&
        process.stdout.isTTY &&
        typeof process.stdin.setRawMode === "function" &&
        process.env.TERM !== "dumb" &&
        columns() >= 7 &&
        (process.stdout.rows || 24) >= 7;
      for (let index = 0; index < choices.length; index += 1) {
        const marker = index === defaultChoice ? " (default)" : "";
        // Full labels remain available in scrollback, even if the live region
        // must truncate a long choice on a narrow terminal.
        if (!interactive)
          writeInkLine(`  ${index + 1}) ${choices[index]}${marker}`);
        else if (
          choices[index].length > columns() - 6 ||
          choices[index].includes("\n")
        )
          writeInkLine(`  • ${choices[index]}`, "muted");
      }

      if (interactive) {
        const selected = await chooseInkKeyboard(
          choices,
          defaultChoice,
          hasColor(),
          (chunk) => input.normalize(chunk),
        );
        writeInkLine(`Selected: ${choices[selected]}`, "muted");
        return selected;
      }

      while (true) {
        const answer = (
          await readLine("Select number: ", input, decoder)
        ).trim();
        if (answer === "" && defaultIndex !== undefined && defaultChoice >= 0)
          return defaultChoice;
        const selected = Number(answer) - 1;
        if (
          Number.isInteger(selected) &&
          selected >= 0 &&
          selected < choices.length
        ) {
          return selected;
        }
        writeInkLine(`Enter a number from 1 to ${choices.length}.`);
      }
    },
    wait(milliseconds) {
      return new Promise((resolve) => setTimeout(resolve, milliseconds));
    },
    runCommand(command) {
      const shell = process.platform === "win32" ? "cmd" : "/bin/sh";
      const args =
        process.platform === "win32"
          ? ["/d", "/s", "/c", command]
          : ["-c", command];
      return spawnCommand(shell, args);
    },
    copy(text) {
      return copyToClipboard(text);
    },
  };
}
