import parseYargs from "yargs-parser";

export interface ParsedArgs {
  command: string[];
  flags: Map<string, string | boolean>;
  positionals: string[];
}

export type OutputFormat = "component" | "json" | "yaml";

const outputFormats: readonly OutputFormat[] = ["component", "json", "yaml"];

const booleanFlags = [
  "help",
  "server-tls",
  "server-insecure",
  "insecure-storage",
  "no-browser",
  "sign",
  "debug",
  "retry",
];

export function parseArgs(argv: string[]): ParsedArgs {
  const flags = new Map<string, string | boolean>();
  const parsed = parseYargs(argv, {
    alias: { h: "help", o: "output", s: "server" },
    boolean: booleanFlags,
    configuration: { "camel-case-expansion": false },
  });
  for (const [key, value] of Object.entries(parsed)) {
    if (key === "_") continue;
    if (typeof value === "boolean") flags.set(key, value);
    else if (typeof value === "string" || typeof value === "number") {
      flags.set(key, String(value));
    }
  }
  for (const name of booleanFlags) {
    if (name.startsWith("no-") && parsed[name.slice(3)] === false) {
      flags.set(name, true);
    }
  }
  const positional = parsed._.map(String);
  return {
    command: positional.slice(0, 2),
    flags,
    positionals: positional.slice(2),
  };
}

export function flagString(
  args: ParsedArgs,
  name: string,
  fallback = "",
): string {
  const value = args.flags.get(name);
  return typeof value === "string" ? value : fallback;
}

export function outputFormat(args: ParsedArgs): OutputFormat {
  const format = flagString(args, "output", "component").toLowerCase();
  if (outputFormats.includes(format as OutputFormat)) {
    return format as OutputFormat;
  }
  throw new Error(
    `Unsupported output format "${format}". Supported output formats: ${outputFormats.join(", ")}`,
  );
}

export function hasFlag(args: ParsedArgs, name: string): boolean {
  return args.flags.get(name) === true || args.flags.get(name) === "true";
}
