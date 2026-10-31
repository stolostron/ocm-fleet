/* eslint-disable playwright/no-standalone-expect */
import { describe, expect, it } from "vitest";
import Upstream from "yoga-layout";

import Local from "./index";

type Node = ReturnType<typeof Local.Node.create>;

function panel(create: () => Node, width: number, selection = false): Node[] {
  const root = create();
  root.setWidth(width);
  const box = create();
  box.setFlexDirection(0);
  box.setBorder(8, 1);
  box.setPadding(6, 1);
  root.insertChild(box, 0);
  const nodes = [root, box];
  for (let index = 0; index < 3; index += 1) {
    const text = create();
    const length = index === 1 ? 103 : 22;
    text.setFlexShrink(1);
    text.setMeasureFunc((available) => ({
      width: Math.min(length, available),
      height: Math.ceil(length / available),
    }));
    if (selection) {
      const row = create();
      row.setFlexDirection(2);
      row.setHeight(1);
      row.insertChild(text, 0);
      box.insertChild(row, index);
      nodes.push(row, text);
    } else {
      if (index === 2) text.setMargin(1, 1);
      box.insertChild(text, index);
      nodes.push(text);
    }
  }
  root.calculateLayout();
  return nodes;
}

function geometry(nodes: Node[]): number[][] {
  return nodes.map((node) => [
    node.getComputedLeft(),
    node.getComputedTop(),
    node.getComputedWidth(),
    node.getComputedHeight(),
    node.getComputedPadding(0),
    node.getComputedBorder(0),
  ]);
}

describe("private layout parity with upstream Yoga", () => {
  it.each([32, 47, 76, 79, 159, 239])(
    "matches bordered, padded, wrapped panels at %i columns",
    (width) => {
      const local = panel(() => Local.Node.create(), width);
      const upstream = panel(
        () => Upstream.Node.create() as unknown as Node,
        width,
      );
      expect(geometry(local)).toEqual(geometry(upstream));
      local[0].freeRecursive();
      upstream[0].freeRecursive();
    },
  );

  it.each([32, 47, 79, 159, 239])(
    "matches fixed-height clipped selection rows at %i columns",
    (width) => {
      const local = panel(() => Local.Node.create(), width, true);
      const upstream = panel(
        () => Upstream.Node.create() as unknown as Node,
        width,
        true,
      );
      expect(geometry(local)).toEqual(geometry(upstream));
      local[0].freeRecursive();
      upstream[0].freeRecursive();
    },
  );

  it("rejects unsupported flex styles instead of silently ignoring them", () => {
    const node = Local.Node.create();
    expect(() => node.setFlexWrap(1)).toThrow("does not support flex wrapping");
    expect(() => node.setFlexDirection(3)).toThrow("row/column only");
    node.free();
  });
});
