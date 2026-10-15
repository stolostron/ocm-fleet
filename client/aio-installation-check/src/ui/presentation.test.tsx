/* eslint-disable playwright/no-standalone-expect */
import { renderToString } from "ink";
import { describe, expect, it } from "vitest";

import { Banner, CheckPanel, Selection } from "./presentation";

describe("inline Ink presentation", () => {
  it.each([32, 76])("keeps branded panels within %i columns", (columns) => {
    const output = renderToString(<Banner color={false} />, { columns });
    expect(output).toContain("OME");
    expect(output).toContain("FleetShift");
    expect(output).toContain("╭");
    expect(output.split("\n").every((line) => line.length <= columns)).toBe(
      true,
    );
    expect(output).not.toContain("\u001b");
  });

  it("renders structured status, requirement, reason and remediation", () => {
    const output = renderToString(
      <CheckPanel
        color={false}
        result={{
          id: "socket",
          title: "Engine socket",
          status: "error",
          blocking: true,
          reason: "Socket unavailable",
          remediation: {
            explanation: "Start engine",
            documentationUrl: "https://example.com/help",
            actions: [
              {
                id: "start",
                title: "Start",
                description: "Start local engine",
                command: "podman machine start",
                manual: false,
                requiresPrivilege: false,
              },
            ],
          },
        }}
      />,
      { columns: 36 },
    );
    expect(output).toContain("ERROR");
    expect(output).toContain("required");
    expect(output).toContain("Socket unavailable");
    expect(output).toContain("podman machine start");
    expect(output.split("\n").every((line) => line.length <= 36)).toBe(true);
  });

  it("marks focused selection without relying on color and bounds menu height", () => {
    const output = renderToString(
      <Selection
        choices={["First", "Second", "Third"]}
        focused={1}
        color={false}
        rows={7}
      />,
      { columns: 32 },
    );
    expect(output).toContain("❯ Second");
    expect(output).toContain("Enter");
    expect(output.split("\n").length).toBeLessThan(7);
  });
});
