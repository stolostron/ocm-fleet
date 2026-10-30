import { createElement } from "react";
import { renderToString } from "react-dom/server";
import { useAuth } from "react-oidc-context";
import { describe, expect, it, vi } from "vitest";

import { OidcAuthInner } from "../../contexts/AuthContext";

vi.mock("react-oidc-context", () => ({
  useAuth: vi.fn(),
  AuthProvider: vi.fn(),
}));
vi.mock("../fetchInterceptor", () => ({
  installFetchInterceptor: vi.fn(),
  setOnUnauthorized: vi.fn(),
}));

describe("OIDC renewal rendering", () => {
  it("keeps authenticated enrollment UI mounted while signinSilent renews", () => {
    vi.mocked(useAuth).mockReturnValue({
      isLoading: true,
      isAuthenticated: true,
    } as ReturnType<typeof useAuth>);
    expect(
      renderToString(
        createElement(OidcAuthInner, { children: "Enrollment in progress" }),
      ),
    ).toContain("Enrollment in progress");
  });

  it("blocks initial unauthenticated loading", () => {
    vi.mocked(useAuth).mockReturnValue({
      isLoading: true,
      isAuthenticated: false,
    } as ReturnType<typeof useAuth>);
    expect(
      renderToString(
        createElement(OidcAuthInner, { children: "Protected content" }),
      ),
    ).toBe("");
  });
});
