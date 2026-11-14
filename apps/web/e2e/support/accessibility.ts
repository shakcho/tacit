import axe, { type AxeResults } from "axe-core";
import type { Page } from "@playwright/test";

type AxeBrowserGlobal = {
  run: (context: Document) => Promise<AxeResults>;
};

declare global {
  var axe: AxeBrowserGlobal | undefined;
}

export async function scanAccessibility(page: Page): Promise<AxeResults> {
  await page.addScriptTag({ content: axe.source });
  return page.evaluate(async () => {
    if (globalThis.axe === undefined) throw new Error("axe-core did not load in the browser");
    return globalThis.axe.run(document);
  });
}
