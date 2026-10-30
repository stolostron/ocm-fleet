import { describe, expect, it } from "vitest";

import { oidcIdentity } from "../oidcIdentity";

describe("OIDC identity", () => {
  it("uses the subject for identity and standard profile claims for labels", () => {
    expect(
      oidcIdentity({
        sub: "stable-subject",
        preferred_username: "operator",
        email: "operator@example.com",
        name: "Platform Operator",
      }),
    ).toEqual({
      id: "stable-subject",
      username: "operator",
      display_name: "Operator",
      navLayout: [],
    });
  });

  it.each([
    [
      { sub: "subject", email: "user@example.com" },
      "user@example.com",
      "user@example.com",
    ],
    [{ sub: "subject", name: "User Name" }, "User Name", "User Name"],
    [
      { sub: "subject", email: "user@example.com", name: "User Name" },
      "user@example.com",
      "User Name",
    ],
    [{ sub: "subject" }, "subject", "subject"],
    [
      { sub: "subject", preferred_username: "ops-user" },
      "ops-user",
      "Ops-user",
    ],
    [
      {
        sub: "subject",
        preferred_username: "",
        email: "user@example.com",
        name: "  ",
      },
      "user@example.com",
      "user@example.com",
    ],
  ])(
    "falls back without provider-specific claims: %j",
    (profile, username, displayName) => {
      expect(oidcIdentity(profile)).toMatchObject({
        id: "subject",
        username,
        display_name: displayName,
      });
    },
  );
});
