/* eslint-disable playwright/no-standalone-expect */
import { expect, it, vi } from "vitest";

import { inputWake } from "./input-wake";

it("owns a native-only wake deadline exactly for the pending prompt", () => {
  vi.useFakeTimers();
  try {
    const nodeStop = inputWake();
    expect(vi.getTimerCount()).toBe(0);
    nodeStop();
    Object.defineProperty(process.versions, "perry", {
      configurable: true,
      value: "0.5.1520",
    });
    const stop = inputWake();
    expect(vi.getTimerCount()).toBe(1);
    stop();
    stop();
    expect(vi.getTimerCount()).toBe(0);
  } finally {
    Reflect.deleteProperty(process.versions, "perry");
    vi.useRealTimers();
  }
});
