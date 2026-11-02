import type { AuthContextProps } from "react-oidc-context";
import { afterEach, describe, expect, it, vi } from "vitest";

import { enrollWithOidc, refreshAndGetIdToken } from "../enrollmentUtils";

vi.mock("../signingKeyApi", () => ({
  signDeployment: vi.fn().mockResolvedValue("signature"),
}));

function authClient(idToken = "renewed-id-token") {
  return {
    user: { id_token: "old-id-token" } as AuthContextProps["user"],
    signinSilent: vi.fn().mockResolvedValue({ id_token: idToken }),
  };
}

afterEach(() => vi.unstubAllGlobals());

describe("OIDC signing enrollment", () => {
  it("renews with the supplied client and sends only the returned ID token", async () => {
    const auth = authClient();
    const fetch = vi.fn().mockImplementation(async (url: string) => {
      if (url === "/v1alpha/signerEnrollments") {
        return Response.json({ name: "signerEnrollments/browser-test" });
      }
      if (url === "/api/ui/verify-sign")
        return Response.json({ verified: true });
      throw new Error(`Unexpected request: ${url}`);
    });
    vi.stubGlobal("fetch", fetch);
    const onEnrolled = vi.fn();

    await enrollWithOidc(auth, onEnrolled);

    expect(auth.signinSilent).toHaveBeenCalledExactlyOnceWith();
    expect(fetch.mock.calls.map(([url]) => url)).toEqual([
      "/v1alpha/signerEnrollments",
      "/api/ui/verify-sign",
    ]);
    expect(JSON.parse(fetch.mock.calls[0][1].body).identity_token).toBe(
      "renewed-id-token",
    );
    expect(fetch.mock.calls[1][1].headers.Authorization).toBe(
      "Bearer renewed-id-token",
    );
    expect(onEnrolled).toHaveBeenCalledWith("signerEnrollments/browser-test");
  });

  it.each([null, {}, { id_token: "" }])(
    "prevents enrollment when renewal returns no ID token: %j",
    async (user) => {
      const auth = authClient();
      auth.signinSilent.mockResolvedValue(user);
      const fetch = vi.fn();
      vi.stubGlobal("fetch", fetch);

      await expect(enrollWithOidc(auth, vi.fn())).rejects.toThrow(/ID token/);
      expect(fetch).not.toHaveBeenCalled();
    },
  );

  it.each([true, false])(
    "accepts a reissued token only when signature verification passes: %s",
    async (verified) => {
      const fetch = vi
        .fn()
        .mockResolvedValueOnce(
          Response.json({ name: "signerEnrollments/test" }),
        )
        .mockResolvedValueOnce(Response.json({ verified }));
      vi.stubGlobal("fetch", fetch);
      const operation = enrollWithOidc(authClient("old-id-token"), vi.fn());
      if (verified) await expect(operation).resolves.toBeUndefined();
      else await expect(operation).rejects.toThrow(/verification failed/i);
      expect(fetch).toHaveBeenCalledTimes(2);
    },
  );

  it("explains how to recover when silent renewal is unsupported", async () => {
    const auth = authClient();
    auth.signinSilent.mockRejectedValue(new Error("login_required"));
    await expect(refreshAndGetIdToken(auth)).rejects.toThrow(
      /sign in again.*login_required/,
    );
  });

  it("reports provider claim provisioning failures without pretending verification succeeded", async () => {
    const fetch = vi
      .fn()
      .mockResolvedValueOnce(Response.json({ name: "signerEnrollments/test" }))
      .mockResolvedValueOnce(
        Response.json({ verified: false, error: "missing signing claim" }),
      );
    vi.stubGlobal("fetch", fetch);
    await expect(enrollWithOidc(authClient(), vi.fn())).rejects.toThrow(
      /provider.*signing.*claim/i,
    );
  });
});
