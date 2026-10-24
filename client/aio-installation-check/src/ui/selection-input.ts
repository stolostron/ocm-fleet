export type SelectionKey = "up" | "down" | "confirm" | "cancel";

// Real terminals can split an escape sequence across multiple reads.
export class SelectionInput {
  private pending = "";

  push(chunk: string): SelectionKey[] {
    this.pending += chunk;
    const keys: SelectionKey[] = [];
    while (this.pending.length > 0) {
      if (this.pending.startsWith("\u001b")) {
        if (this.pending.length === 1) break;
        if (this.pending[1] === "[" || this.pending[1] === "O") {
          const match = /^\u001b[\[O][0-9;]*([A-Za-z~])/.exec(this.pending);
          if (!match) {
            if (this.pending.length < 32) break;
            this.pending = this.pending.slice(1);
            continue;
          }
          if (match[1] === "A") keys.push("up");
          if (match[1] === "B") keys.push("down");
          this.pending = this.pending.slice(match[0].length);
          continue;
        }
      }
      const character = this.pending[0];
      this.pending = this.pending.slice(1);
      if (character === "\u0003") keys.push("cancel");
      if (character === "\r" || character === "\n") keys.push("confirm");
    }
    return keys;
  }
}
