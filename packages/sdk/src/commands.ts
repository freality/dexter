import { DexError } from "./errors.js";
import type { CommandSpec } from "./types.js";

export interface CommandRegistry {
  define(name: string, spec: CommandSpec): void;
  get(name: string): CommandSpec | undefined;
  names(): string[];
}

/**
 * Registry backing defineCommand (D13). Names are unique and list in sorted
 * order so `dex run` with no arguments prints deterministically.
 */
export function createCommandRegistry(): CommandRegistry {
  const commands = new Map<string, CommandSpec>();
  return Object.freeze({
    define(name: string, spec: CommandSpec): void {
      if (commands.has(name)) {
        throw new DexError("DUPLICATE_COMMAND", `command "${name}" is already defined`);
      }
      commands.set(name, spec);
    },
    get(name: string): CommandSpec | undefined {
      return commands.get(name);
    },
    names(): string[] {
      return [...commands.keys()].sort();
    },
  });
}
