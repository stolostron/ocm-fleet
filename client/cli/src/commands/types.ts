import type { ReactNode } from "react";

import type { ParsedArgs } from "../argv";

/** Parsed input passed to a command's runner. */
export interface CommandContext {
  args: ParsedArgs;
}

/** Performs the command's work and returns data for the output layer. */
export type CommandRunner<Data> = (context: CommandContext) => Promise<Data>;

/** Produces human output; keep React hooks inside returned components. */
export type CommandPresenter<Data> = (data: Data) => ReactNode;

/** Names and help text shared by runnable commands and command groups. */
interface CommandMetadata {
  path: string;
  aliases?: string[];
  description: string;
}

/** Defines a runnable command whose runner and human presenter share one data type. */
export interface CommandSpec<Data> extends CommandMetadata {
  implemented: true;
  run: CommandRunner<Data>;
  /** Optional human-oriented Ink/React child presentation. */
  present?: CommandPresenter<Data>;
}

/** Stores a runnable command in the registry, preserving typed access to its spec. */
export interface RegisteredCommand extends CommandMetadata {
  implemented: true;
  /**
   * Visit the complete spec without assuming which data type this command uses.
   * The generic callback encodes an existential type: Data is hidden but consistent within the spec.
   * @see https://github.com/microsoft/TypeScript/issues/14466
   */
  withSpec<Result>(visit: <Data>(spec: CommandSpec<Data>) => Result): Result;
}

/** A registry entry: either a runnable command or metadata for a group or unimplemented command. */
export type CommandEntry =
  RegisteredCommand | (CommandMetadata & { implemented?: false });
