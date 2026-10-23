/* eslint-disable playwright/no-standalone-expect */
import { cleanup, render } from "ink-testing-library";
import { afterEach, describe, expect, it } from "vitest";

import { DeploymentListView } from "../commands/deployment/list";
import { Output } from "../ui";

afterEach(cleanup);

describe("Ink output", () => {
  it("renders command output in terminal frame", () => {
    const instance = render(<Output data="fleetctl ready" />);
    expect(instance.lastFrame()).toContain("fleetctl ready");
  });

  it("renders deployment data with human defaults and a JSON hint", () => {
    const instance = render(
      <DeploymentListView data={[{ name: "example" }]} />,
    );

    expect(instance.lastFrame()).toContain("example");
    expect(instance.lastFrame()).toContain("UNKNOWN");
    expect(instance.lastFrame()).toContain("NONE");
    expect(instance.lastFrame()).toContain(
      "Use --output json or yaml for full details.",
    );
  });

  it("renders the empty deployment list", () => {
    const instance = render(<DeploymentListView data={[]} />);

    expect(instance.lastFrame()).toContain(
      "Use --output json or yaml for full details.",
    );
  });
});
