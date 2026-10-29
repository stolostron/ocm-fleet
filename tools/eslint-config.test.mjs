import assert from "node:assert/strict";
import { join } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { ESLint } from "eslint";

const repository = fileURLToPath(new URL("../", import.meta.url));
const eslint = new ESLint({ cwd: repository });

test("Vitest assertions do not inherit Playwright rules", async (t) => {
  for (const path of [
    "client/web/src/example.test.ts",
    "sdk/common/src/example.test.ts",
    "sdk/build-utils/src/example.test.ts",
    "e2e/cli/tests/steps/example.test.ts",
    "e2e/cli/tests/support/example.test.ts",
    "e2e/shared/example.test.ts",
    "e2e/sandbox/example.test.mjs",
  ]) {
    await t.test(path, async () => {
      assert.deepEqual(
        await lint(
          path,
          `import { expect, it } from "vitest";

it("checks a value", () => {
  expect(1).toBe(1);
});
`,
        ),
        [],
      );
    });
  }
});

test("Node assertions do not inherit Playwright rules", async () => {
  assert.deepEqual(
    await lint(
      "tools/example.test.mjs",
      `import assert from "node:assert/strict";
import test from "node:test";

test("checks a value", () => {
  assert.equal(1, 1);
});
`,
    ),
    [],
  );
});

test("Playwright component and end-to-end files retain their rules", async (t) => {
  for (const [path, runner] of [
    ["client/web/src/example.ct.tsx", "@playwright/experimental-ct-react"],
    [
      "extensions/core/client/src/example.ct.tsx",
      "@playwright/experimental-ct-react",
    ],
    ["e2e/cli/tests/scenarios/example.spec.ts", "@playwright/test"],
    ["e2e/web/tests/example.spec.ts", "@playwright/test"],
    ["e2e/web/tests/auth-setup.ts", "@playwright/test"],
    ["e2e/cli/tests/steps/example.ts", "@playwright/test"],
    ["e2e/shared/example.ts", "@playwright/test"],
  ]) {
    await t.test(path, async () => {
      const messages = await lint(
        path,
        `import { expect, test } from "${runner}";

test("checks a page", async ({ page }) => {
  await page.waitForTimeout(500);
  await expect(page).toHaveTitle("Example");
});
`,
      );
      assert.deepEqual(
        messages.map(({ ruleId }) => ruleId),
        ["playwright/no-wait-for-timeout"],
      );
    });
  }
});

test("Vitest files retain ordinary TypeScript lint rules", async () => {
  const messages = await lint(
    "e2e/cli/tests/steps/example.test.ts",
    "export const value: any = 1;\n",
  );
  assert.deepEqual(
    messages.map(({ ruleId }) => ruleId),
    ["@typescript-eslint/no-explicit-any"],
  );
});

async function lint(path, source) {
  const [result] = await eslint.lintText(source, {
    filePath: join(repository, path),
  });
  return result.messages;
}
