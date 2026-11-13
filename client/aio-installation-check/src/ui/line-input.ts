import { TerminalCancelled } from "./keyboard-selection";

// State belongs to one terminal, including buffered pipe input and a CRLF that
// arrives across prompt boundaries. EOF must not poison future terminal instances.
export class LineInput {
  private pending = "";
  private afterCR = false;
  text = "";
  ended = false;

  normalize(chunk: string): string {
    let normalized = "";
    for (const character of chunk) {
      if (character === "\n" && this.afterCR) {
        this.afterCR = false;
        continue;
      }
      this.afterCR = character === "\r";
      normalized += this.afterCR ? "\n" : character;
    }
    return normalized;
  }

  push(chunk: string): void {
    this.pending += this.normalize(chunk);
  }

  take(editing: boolean): string | undefined {
    while (this.pending.length > 0) {
      if (editing && this.pending.startsWith("\u001b")) {
        // Ignore terminal escape sequences, including bracketed-paste wrappers;
        // retain incomplete sequences until the next read.
        const sequence = /^\u001b[\[O][0-9;]*[A-Za-z~]/.exec(this.pending);
        if (!sequence && /^\u001b(?:[\[O][0-9;]*)?$/.test(this.pending)) break;
        this.pending = this.pending.slice(sequence ? sequence[0].length : 1);
        continue;
      }
      const character = String.fromCodePoint(this.pending.codePointAt(0)!);
      this.pending = this.pending.slice(character.length);
      if (character === "\u0003") {
        this.pending = "";
        this.text = "";
        throw new TerminalCancelled();
      }
      if (character === "\n") {
        const line = this.text;
        this.text = "";
        return line;
      }
      if (editing && (character === "\u007f" || character === "\b")) {
        const characters = Array.from(this.text);
        characters.pop();
        this.text = characters.join("");
      } else if (!editing || character >= " " || character === "\t") {
        this.text += character;
      }
    }
    if (this.ended) {
      if (this.text.length === 0)
        throw new Error("Input closed before confirmation.");
      const line = this.text;
      this.text = "";
      return line;
    }
    return undefined;
  }
}
