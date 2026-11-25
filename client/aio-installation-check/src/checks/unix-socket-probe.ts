import { createConnection } from "node:net";

import type { UnixSocketProbeResult } from "./dependencies.js";

const PING_REQUEST =
  "GET /_ping HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n";

export function probeUnixSocket(path: string): Promise<UnixSocketProbeResult> {
  return new Promise((resolve) => {
    const socket = createConnection({ path });
    let connected = false;
    let settled = false;
    let response = "";

    const finish = (status: 0 | 1 | 2): void => {
      if (settled) return;
      settled = true;
      socket.destroy();
      resolve({ isSocket: status > 0, responds: status === 2 });
    };

    socket.setTimeout(2_000, () => finish(connected ? 1 : 0));
    socket.once("connect", () => {
      connected = true;
      socket.write(PING_REQUEST);
    });
    socket.on("data", (chunk: Uint8Array) => {
      response += new TextDecoder().decode(chunk);
      const newline = response.indexOf("\n");
      if (newline < 0) return;
      const firstLine = response.slice(0, newline).replace(/\r$/, "");
      finish(/^HTTP\/1\.[01] 200\b/.test(firstLine) ? 2 : 1);
    });
    socket.once("error", () => finish(connected ? 1 : 0));
    socket.once("close", () => finish(connected ? 1 : 0));
  });
}
