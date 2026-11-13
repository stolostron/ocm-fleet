import { z } from "zod";

import type { ConsoleConfig, ConsoleConfigSource } from "./types";

const nonempty = z.string().min(1);
const page = z.object({
  type: z.literal("page"),
  pageId: nonempty,
  iconOverride: z.string().optional(),
});
const extension = z
  .object({
    type: nonempty,
    properties: z.record(z.string(), z.unknown()),
    flags: z
      .object({
        required: z.array(z.string()).optional(),
        disallowed: z.array(z.string()).optional(),
      })
      .optional(),
  })
  .passthrough();
const metadata = {
  name: nonempty,
  version: nonempty,
  extensions: z.array(extension),
  dependencies: z.record(z.string(), z.string()).optional(),
  optionalDependencies: z.record(z.string(), z.string()).optional(),
  customProperties: z.record(z.string(), z.unknown()).optional(),
};
const manifest = z.discriminatedUnion("registrationMethod", [
  z
    .object({ ...metadata, registrationMethod: z.literal("local") })
    .passthrough(),
  z
    .object({
      ...metadata,
      registrationMethod: z.literal("callback"),
      baseURL: nonempty,
      loadScripts: z.array(nonempty),
      buildHash: z.string().optional(),
    })
    .passthrough(),
  z
    .object({
      ...metadata,
      registrationMethod: z.literal("custom"),
      baseURL: nonempty,
      loadScripts: z.array(nonempty),
      buildHash: z.string().optional(),
    })
    .passthrough(),
]);
const schema = z.object({
  schemaVersion: z.literal(1),
  auth: z.discriminatedUnion("type", [
    z.strictObject({ type: z.literal("none") }),
    z.strictObject({
      type: z.literal("oidc"),
      authority: nonempty,
      clientId: nonempty,
      scope: z.string().optional(),
    }),
  ]),
  catalog: z.object({
    pluginEntries: z.array(
      z.object({
        key: nonempty,
        label: z.string().optional(),
        pluginManifest: manifest,
      }),
    ),
  }),
  defaultNavLayout: z
    .array(
      z.discriminatedUnion("type", [
        page,
        z.object({
          type: z.literal("group"),
          groupId: nonempty,
          pluginKey: nonempty,
          label: z.string(),
          children: z.array(page),
        }),
        z.object({
          type: z.literal("section"),
          id: nonempty,
          label: z.string(),
          children: z.array(z.object({ pageId: nonempty })),
        }),
      ]),
    )
    .optional(),
});

export function validateConfig(value: unknown): ConsoleConfig {
  const result = schema.safeParse(value);
  if (!result.success) {
    throw new Error(
      `Invalid console config: ${result.error.issues.map((issue) => `${issue.path.join(".")}: ${issue.message}`).join("; ")}`,
    );
  }
  const entries = result.data.catalog.pluginEntries;
  if (
    new Set(entries.map((entry) => entry.key)).size !== entries.length ||
    new Set(entries.map((entry) => entry.pluginManifest.name)).size !==
      entries.length
  ) {
    throw new Error(
      "Invalid console config: plugin keys and manifest names must be unique",
    );
  }
  return result.data;
}

export async function loadConfig(
  source: ConsoleConfigSource,
  signal: AbortSignal,
): Promise<ConsoleConfig> {
  let value: unknown;
  if (source.type === "static") value = source.config;
  else if (source.type === "factory") value = await source.load();
  else {
    const response = await fetch(source.url, { signal });
    if (!response.ok)
      throw new Error(`Failed to load console config: HTTP ${response.status}`);
    value = await response.json();
  }
  return validateConfig(value);
}
