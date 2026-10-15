import stringWidth from "string-width";

// Raw-mode output must not depend on the terminal driver's OPOST/ONLCR flags.
export function terminalNewlines(text: string): string {
  return text.replace(/\r?\n/g, "\r\n");
}

function physicalColumns(columns?: number): number {
  return typeof columns === "number" && Number.isFinite(columns) && columns >= 1
    ? Math.floor(columns)
    : 80;
}

export function terminalColumns(columns?: number): number {
  // Leave the last physical column unused: writing it enables terminal autowrap.
  // A one-column terminal cannot reserve a column; never inflate its real width.
  return Math.max(1, physicalColumns(columns) - 1);
}

export class InlineRegion {
  private widths: number[] = [];

  draw(frame: string, columns: number, rows: number): string {
    columns = physicalColumns(columns);
    const physicalRows = this.widths.reduce(
      (sum, width) => sum + Math.max(1, Math.ceil(width / columns)),
      0,
    );
    const eraseRows = Math.min(physicalRows, Math.max(0, rows - 1));
    let output = "";
    for (let line = 0; line < eraseRows; line += 1)
      output += "\u001b[1A\r\u001b[2K";
    const lines = frame.split("\n");
    this.widths = lines.map((line) => stringWidth(line));
    if (lines.length >= rows || this.widths.some((width) => width >= columns))
      throw new Error("Inline menu exceeds its physical terminal region.");
    return output + "\r" + terminalNewlines(frame) + "\r\n";
  }
}
