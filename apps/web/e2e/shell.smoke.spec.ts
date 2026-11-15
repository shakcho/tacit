import { expect, test } from "@playwright/test";

import { scanAccessibility } from "./support/accessibility";

test("authenticated shell renders and supports keyboard navigation", async ({ page }, testInfo) => {
  test.skip(!process.env.WORKOS_CLIENT_ID, "WorkOS staging credentials are required for the authenticated browser flow");
  await page.goto("/meetings");

  await expect(page.getByRole("heading", { level: 1, name: "Meetings, ready to review." })).toBeVisible();
  const productNavigation = page.locator('nav[aria-label="Product"]:visible');
  await expect(productNavigation).toBeVisible();
  await expect(productNavigation.getByRole("link", { name: "Meetings", exact: true })).toHaveAttribute("aria-current", "page");

  await page.keyboard.press("Tab");
  const skipLink = page.getByRole("link", { name: "Skip to content" });
  await expect(skipLink).toBeFocused();
  await skipLink.press("Enter");
  await expect(page.locator("#main-content")).toBeFocused();

  const accessibility = await scanAccessibility(page);
  expect(accessibility.violations).toEqual([]);

  await testInfo.attach("authenticated-shell", {
    body: await page.screenshot({ fullPage: true }),
    contentType: "image/png",
  });
});

test("public share stays separate from authenticated navigation", async ({ page }, testInfo) => {
  await page.goto("/share/smoke-share");

  await expect(page.getByRole("heading", { level: 1, name: "Shared meeting review" })).toBeVisible();
  await expect(page.getByText("Shared view")).toBeVisible();
  await expect(page.getByRole("navigation", { name: "Product" })).toHaveCount(0);

  const accessibility = await scanAccessibility(page);
  expect(accessibility.violations).toEqual([]);

  await testInfo.attach("public-share-shell", {
    body: await page.screenshot({ fullPage: true }),
    contentType: "image/png",
  });
});
