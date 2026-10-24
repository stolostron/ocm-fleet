// Private Perry layout subset for AIO's inline Ink panels. Upstream Yoga remains
// authoritative for Node. Supported: row/column flex, dimensions, borders,
// padding, margins, alignment and fixed-height clipping. Unsupported styles fail
// explicitly rather than silently producing misleading layout.
type Measure = (
  width: number,
  widthMode: number,
  height: number,
  heightMode: number,
) => { width: number; height: number };

class YogaNode {
  private children: YogaNode[] = [];
  private measure?: Measure;
  private width?: number;
  private height?: number;
  private minWidth = 0;
  private minHeight = 0;
  private direction = 0;
  private grow = 0;
  private shrink = 0;
  private align = 4;
  private display = 0;
  private padding = [0, 0, 0, 0];
  private border = [0, 0, 0, 0];
  private margin = [0, 0, 0, 0];
  private computedWidth = 0;
  private computedHeight = 0;
  private left = 0;
  private top = 0;

  setMeasureFunc(callback: Measure): void {
    this.measure = callback;
  }
  unsetMeasureFunc(): void {
    this.measure = undefined;
  }
  setWidth(value: number): void {
    this.width = value;
  }
  setWidthAuto(): void {
    this.width = undefined;
  }
  setHeight(value: number): void {
    this.height = value;
  }
  setHeightAuto(): void {
    this.height = undefined;
  }
  setMinWidth(value: number): void {
    this.minWidth = value;
  }
  setMinHeight(value: number): void {
    this.minHeight = value;
  }
  setFlexDirection(value: number): void {
    if (value !== 0 && value !== 2)
      throw new Error("AIO layout supports row/column only");
    this.direction = value;
  }
  setFlexGrow(value: number): void {
    this.grow = value;
  }
  setFlexShrink(value: number): void {
    this.shrink = value;
  }
  setAlignItems(value: number): void {
    this.align = value;
  }
  setFlexWrap(value: number): void {
    if (value !== 0)
      throw new Error("AIO layout does not support flex wrapping");
  }
  setDisplay(value: number): void {
    this.display = value;
  }
  getDisplay(): number {
    return this.display;
  }
  private edge(values: number[], edge: number, value: number): void {
    if (edge === 8) {
      values.fill(value);
      return;
    }
    if (edge === 6) {
      values[0] = value;
      values[2] = value;
      return;
    }
    if (edge === 7) {
      values[1] = value;
      values[3] = value;
      return;
    }
    values[edge === 4 ? 0 : edge === 5 ? 2 : edge] = value;
  }
  setPadding(edge: number, value: number): void {
    this.edge(this.padding, edge, value);
  }
  setBorder(edge: number, value: number): void {
    this.edge(this.border, edge, value);
  }
  setMargin(edge: number, value: number): void {
    this.edge(this.margin, edge, value);
  }
  insertChild(child: YogaNode, index: number): void {
    this.children.splice(index, 0, child);
  }
  removeChild(child: YogaNode): void {
    const index = this.children.indexOf(child);
    if (index >= 0) this.children.splice(index, 1);
  }
  getChildCount(): number {
    return this.children.length;
  }
  // There is no cached layout: every calculation measures the current tree.
  markDirty(): void {}

  calculateLayout(width?: number, height?: number): void {
    this.layout(width, height, this.width !== undefined);
  }

  private layout(
    availableWidth?: number,
    availableHeight?: number,
    stretch = false,
  ): void {
    if (this.display === 1) {
      this.computedWidth = 0;
      this.computedHeight = 0;
      return;
    }
    const horizontal =
      this.padding[0] + this.padding[2] + this.border[0] + this.border[2];
    const vertical =
      this.padding[1] + this.padding[3] + this.border[1] + this.border[3];
    const width = this.width ?? (stretch ? availableWidth : undefined);
    const innerWidth = Math.max(
      0,
      (width ?? availableWidth ?? Number.MAX_SAFE_INTEGER) - horizontal,
    );
    if (this.measure) {
      const measured = this.measure(
        innerWidth,
        width === undefined ? 2 : 1,
        availableHeight ?? Number.MAX_SAFE_INTEGER,
        2,
      );
      this.computedWidth = Math.max(
        this.minWidth,
        width ?? measured.width + horizontal,
      );
      this.computedHeight = Math.max(
        this.minHeight,
        this.height ?? measured.height + vertical,
      );
      return;
    }
    const row = this.direction === 2;
    const children = this.children.filter((child) => child.display !== 1);
    for (const child of this.children) {
      child.layout(
        Math.max(0, innerWidth - child.margin[0] - child.margin[2]),
        undefined,
        !row && this.align === 4,
      );
    }
    const mainSize = (child: YogaNode): number =>
      row ? child.computedWidth : child.computedHeight;
    const mainMargins = (child: YogaNode): number =>
      row
        ? child.margin[0] + child.margin[2]
        : child.margin[1] + child.margin[3];
    const total = children.reduce(
      (sum, child) => sum + mainSize(child) + mainMargins(child),
      0,
    );
    const target = row
      ? width === undefined
        ? undefined
        : innerWidth
      : this.height === undefined
        ? undefined
        : Math.max(0, this.height - vertical);
    if (target !== undefined && total !== target) {
      const free = target - total;
      const weight = children.reduce(
        (sum, child) =>
          sum + (free > 0 ? child.grow : child.shrink * mainSize(child)),
        0,
      );
      if (weight > 0)
        for (const child of children) {
          const share =
            (free * (free > 0 ? child.grow : child.shrink * mainSize(child))) /
            weight;
          const size = Math.max(0, mainSize(child) + share);
          if (row) child.layout(size, undefined, true);
          else child.computedHeight = size;
        }
    }
    let main = 0;
    let cross = 0;
    for (const child of children) {
      child.left =
        this.border[0] + this.padding[0] + child.margin[0] + (row ? main : 0);
      child.top =
        this.border[1] + this.padding[1] + child.margin[1] + (row ? 0 : main);
      main += mainSize(child) + mainMargins(child);
      cross = Math.max(
        cross,
        row
          ? child.computedHeight + child.margin[1] + child.margin[3]
          : child.computedWidth + child.margin[0] + child.margin[2],
      );
    }
    this.computedWidth = Math.max(
      this.minWidth,
      width ?? (row ? main : cross) + horizontal,
    );
    this.computedHeight = Math.max(
      this.minHeight,
      this.height ?? (row ? cross : main) + vertical,
    );
    for (const child of children) {
      const crossFree = row
        ? this.computedHeight -
          vertical -
          child.computedHeight -
          child.margin[1] -
          child.margin[3]
        : this.computedWidth -
          horizontal -
          child.computedWidth -
          child.margin[0] -
          child.margin[2];
      const offset =
        this.align === 2 ? crossFree / 2 : this.align === 3 ? crossFree : 0;
      if (row) child.top += Math.round(offset);
      else child.left += Math.round(offset);
    }
  }
  getComputedLeft(): number {
    return Math.round(this.left);
  }
  getComputedTop(): number {
    return Math.round(this.top);
  }
  getComputedWidth(): number {
    return Math.round(this.computedWidth);
  }
  getComputedHeight(): number {
    return Math.round(this.computedHeight);
  }
  getComputedPadding(edge: number): number {
    return this.padding[edge === 4 ? 0 : edge === 5 ? 2 : edge] ?? 0;
  }
  getComputedBorder(edge: number): number {
    return this.border[edge === 4 ? 0 : edge === 5 ? 2 : edge] ?? 0;
  }
  free(): void {
    this.children = [];
    this.measure = undefined;
  }
  freeRecursive(): void {
    for (const child of this.children) child.freeRecursive();
    this.free();
  }
}

const Yoga = {
  DIRECTION_LTR: 1,
  DISPLAY_FLEX: 0,
  DISPLAY_NONE: 1,
  FLEX_DIRECTION_COLUMN: 0,
  FLEX_DIRECTION_ROW: 2,
  ALIGN_AUTO: 0,
  ALIGN_FLEX_START: 1,
  ALIGN_CENTER: 2,
  ALIGN_FLEX_END: 3,
  ALIGN_STRETCH: 4,
  WRAP_NO_WRAP: 0,
  WRAP_WRAP: 1,
  WRAP_WRAP_REVERSE: 2,
  EDGE_LEFT: 0,
  EDGE_TOP: 1,
  EDGE_RIGHT: 2,
  EDGE_BOTTOM: 3,
  EDGE_START: 4,
  EDGE_END: 5,
  EDGE_HORIZONTAL: 6,
  EDGE_VERTICAL: 7,
  EDGE_ALL: 8,
  Node: { create: () => new YogaNode() },
};
export default Yoga;
