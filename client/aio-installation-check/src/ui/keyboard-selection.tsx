// Perry needs this explicit import to lower JSX as React elements, not eager calls.
import "react/jsx-runtime";

import { StringDecoder } from "node:string_decoder";

import { renderToString } from "ink";

import { InlineRegion, terminalColumns } from "./inline-region";
import { inputWake } from "./input-wake";
import { Selection } from "./presentation";
import { acquireRawMode } from "./raw-mode";
import { SelectionInput } from "./selection-input";

export class TerminalCancelled extends Error {
  constructor() {
    super("Cancelled.");
  }
}

// Ink owns all presentation. This small input/redraw transport keeps the live
// region bounded without Ink's native-unstable useInput/throttled rendering path.
export function chooseInkKeyboard(
  choices: string[],
  initial: number,
  color: boolean,
  normalize: (chunk: string) => string = (chunk) => chunk,
): Promise<number> {
  return new Promise((resolve, reject) => {
    const stopWake = inputWake();
    // Mount after the Promise executor returns: Perry's nested synchronous
    // React work can otherwise leave a spurious pending rejection (value 0).
    setImmediate(() => {
      const state = { focused: initial, settled: false };
      const region = new InlineRegion();
      let restoreRaw = (): void => {};
      const parser = new SelectionInput();
      const decoder = new StringDecoder("utf8");
      let frames: string[] = [];
      const prepare = (): void => {
        frames = [];
        for (let focused = 0; focused < choices.length; focused += 1) {
          frames.push(
            renderToString(
              <Selection
                choices={choices}
                focused={focused}
                color={color}
                rows={process.stdout.rows || 24}
              />,
              { columns: terminalColumns(process.stdout.columns) },
            ),
          );
        }
      };
      const draw = (): void => {
        process.stdout.write(
          region.draw(
            frames[state.focused],
            process.stdout.columns || 80,
            process.stdout.rows || 24,
          ),
        );
      };
      const finish = (error?: Error): void => {
        if (state.settled) return;
        state.settled = true;
        stopWake();
        process.stdin.removeListener("data", onData);
        process.stdin.removeListener("end", ended);
        process.stdout.removeListener("resize", resized);
        process.removeListener("SIGINT", cancel);
        process.removeListener("SIGTERM", cancel);
        try {
          restoreRaw();
        } catch (restoreError) {
          error ??=
            restoreError instanceof Error
              ? restoreError
              : new Error(String(restoreError));
        } finally {
          process.stdin.pause();
          process.stdout.write("\u001b[?25h");
        }
        if (error) reject(error);
        else resolve(state.focused);
      };
      const fail = (error: unknown): void =>
        finish(error instanceof Error ? error : new Error(String(error)));
      const cancel = (): void => finish(new TerminalCancelled());
      const ended = (): void =>
        finish(new Error("Input closed before confirmation."));
      const resized = (): void => {
        try {
          prepare();
          draw();
        } catch (error) {
          fail(error);
        }
      };
      const onData = (chunk: Uint8Array | string): void => {
        try {
          const keys = parser.push(
            normalize(typeof chunk === "string" ? chunk : decoder.write(chunk)),
          );
          for (const key of keys) {
            if (state.settled) return;
            if (key === "cancel") {
              cancel();
              return;
            }
            if (key === "confirm") {
              finish();
              return;
            }
            state.focused =
              (state.focused + (key === "up" ? -1 : 1) + choices.length) %
              choices.length;
            draw();
          }
        } catch (error) {
          fail(error);
        }
      };
      process.on("SIGINT", cancel);
      process.on("SIGTERM", cancel);
      process.stdout.on("resize", resized);
      process.stdin.on("end", ended);
      try {
        prepare();
        restoreRaw = acquireRawMode();
        process.stdout.write("\u001b[?25l");
        draw();
        process.stdin.on("data", onData);
        process.stdin.resume();
      } catch (error) {
        fail(error);
      }
    });
  });
}
