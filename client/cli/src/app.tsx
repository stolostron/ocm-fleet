import type { Writable } from "node:stream";

import { render } from "ink";
import { dump } from "js-yaml";
import type { ReactNode } from "react";

import type { OutputFormat } from "./argv";
import type {
  CommandContext,
  CommandPresenter,
  CommandSpec,
} from "./commands/types";

interface FormatterRegistry {
  component: <Data>(data: Data, present?: CommandPresenter<Data>) => ReactNode;
  json: <Data>(data: Data) => string;
  yaml: <Data>(data: Data) => string;
}

const serializeJson = (data: unknown): string =>
  JSON.stringify(data, null, 2) ?? "null";

const formatters: FormatterRegistry = {
  component: (data, present) => (present ? present(data) : serializeJson(data)),
  json: (data) => serializeJson(data),
  yaml: (data) => dump(data === undefined ? null : data).replace(/\n$/, ""),
};

export async function executeCommand<Data>(
  spec: CommandSpec<Data>,
  context: CommandContext,
  output: OutputFormat = "component",
): Promise<ReactNode> {
  const data = await spec.run(context);
  return formatters[output](data, spec.present);
}

/** Write exact text without terminal wrapping that would split identifiers. */
export async function writeText(
  value: string,
  stdout: Writable = process.stdout,
): Promise<void> {
  await new Promise<void>((resolve, reject) => {
    stdout.once("error", reject);
    stdout.write(`${value}\n`, (error) => {
      if (error) {
        // A failed write also emits "error"; leave the listener to consume it.
        reject(error);
      } else {
        stdout.off("error", reject);
        resolve();
      }
    });
  });
}

export async function print(content: ReactNode): Promise<void> {
  if (typeof content === "string") await writeText(content);
  else await printView(content);
}

export async function printView(element: ReactNode): Promise<void> {
  const instance = render(element);
  try {
    await instance.waitUntilExit();
  } finally {
    instance.unmount();
  }
}
