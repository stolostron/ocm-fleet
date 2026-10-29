import { describe, expect, it } from "vitest";

import {
  Action,
  enrollmentReducer,
  EnrollStep,
  initialState,
} from "../enrollmentReducer";

describe("enrollment transitions", () => {
  it("requires verification again when opening an existing key", () => {
    const previous = {
      ...initialState,
      step: EnrollStep.Enrolled,
      enrollmentName: "signerEnrollments/previous",
    };
    expect(
      enrollmentReducer(previous, {
        type: Action.ExistingKey,
        sshPublicKey: "ssh-key",
        derPublicKey: "der-key",
      }),
    ).toMatchObject({ step: EnrollStep.RegisterKey, enrollmentName: null });
  });

  it("clears apparent enrollment success on verification failure and retry", () => {
    const verifying = enrollmentReducer(initialState, {
      type: Action.EnrollSuccess,
      enrollmentName: "signerEnrollments/test",
    });
    expect(verifying.step).toBe(EnrollStep.Verifying);
    const failed = enrollmentReducer(verifying, {
      type: Action.Error,
      error: "Verification failed",
    });
    expect(failed.step).toBe(EnrollStep.Error);
    expect(failed.enrollmentName).toBeNull();
    expect(
      enrollmentReducer(failed, { type: Action.Retry }).enrollmentName,
    ).toBeNull();
  });
});
