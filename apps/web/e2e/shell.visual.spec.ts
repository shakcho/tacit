import { expect, test } from "@playwright/test";

test("shell keeps equal content edges and no horizontal overflow", async ({ page }, testInfo) => {
  await page.goto("/meetings");

  const viewport = page.viewportSize();
  expect(viewport).not.toBeNull();

  const measurements = await page.locator("body").evaluate((body) => ({
    clientWidth: body.ownerDocument.documentElement.clientWidth,
    scrollWidth: body.ownerDocument.documentElement.scrollWidth,
  }));
  expect(measurements.scrollWidth).toBe(measurements.clientWidth);

  const main = page.locator("#main-content");
  const box = await main.boundingBox();
  expect(box).not.toBeNull();
  if (box !== null && viewport !== null) {
    expect(box.x).toBeGreaterThanOrEqual(viewport.width < 768 ? 16 : 256);
    expect(box.x + box.width).toBeLessThanOrEqual(viewport.width);
  }

  await testInfo.attach(`shell-${testInfo.project.name}`, {
    body: await page.screenshot({ fullPage: true }),
    contentType: "image/png",
  });
});

test("mobile shell exposes four reachable product destinations", async ({ page }) => {
  test.skip(page.viewportSize()?.width !== 390, "mobile-only contract");
  await page.goto("/meetings");

  const navigation = page.locator('nav[aria-label="Product"]:visible');
  await expect(navigation).toBeVisible();
  await expect(navigation.getByRole("link")).toHaveCount(4);

  for (const link of await navigation.getByRole("link").all()) {
    const box = await link.boundingBox();
    expect(box).not.toBeNull();
    if (box !== null) expect(box.height).toBeGreaterThanOrEqual(44);
  }
});
