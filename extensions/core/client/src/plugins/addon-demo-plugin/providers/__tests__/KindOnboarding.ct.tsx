import { expect, test } from "@playwright/experimental-ct-react";

import { KindOnboardingForm } from "../KindOnboarding";

test("describes the API port's automatic assignment to assistive technology", async ({
  mount,
  page,
}) => {
  await mount(<KindOnboardingForm onComplete={() => {}} onCancel={() => {}} />);

  const port = page.getByRole("spinbutton", { name: "API server port" });
  await expect(port).toHaveValue("0");
  await expect(port).toHaveAccessibleDescription("Leave 0 for auto-assign");
});
