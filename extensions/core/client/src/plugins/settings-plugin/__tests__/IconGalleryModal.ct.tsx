import { expect, test } from "@playwright/experimental-ct-react";

import IconGalleryModal from "../IconGalleryModal";

test("finds icons by fuzzy keywords and clears obsolete search results", async ({
  mount,
  page,
}) => {
  await mount(
    <IconGalleryModal
      isOpen
      selected={null}
      onSelect={() => {}}
      onClose={() => {}}
    />,
  );
  const query = page.getByRole("textbox", { name: "Search icons" });
  // Spaces and a typo require the real Orama index rather than its substring fallback.
  await query.fill("cloud uploadd");
  const icon = page.getByRole("button", {
    name: "CloudUploadAltIcon",
    exact: true,
  });
  await expect(icon).toBeVisible();
  await query.fill("zzzzzzzzzzzz");
  await expect(icon).toHaveCount(0);
  await expect(page.getByText("No icons match")).toBeVisible();
  await query.clear();
  await expect(page.getByRole("grid", { name: "Icon gallery" })).toBeVisible();
});
