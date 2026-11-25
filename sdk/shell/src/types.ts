import type { PluginManifest } from "@openshift/dynamic-plugin-sdk";

/** Public bootstrap metadata only. Never include client secrets or tokens. */
export type ConsoleAuthConfig =
  | { type: "none" }
  | { type: "oidc"; authority: string; clientId: string; scope?: string };

export type ConsolePluginManifest = PluginManifest;

export interface ConsolePluginEntry {
  key: string;
  pluginManifest: ConsolePluginManifest;
  label?: string;
}

export interface ConsoleCatalog {
  pluginEntries: ConsolePluginEntry[];
}

export type ConsoleNavLayoutEntry =
  | { type: "page"; pageId: string; iconOverride?: string }
  | {
      type: "group";
      groupId: string;
      pluginKey: string;
      label: string;
      children: { type: "page"; pageId: string; iconOverride?: string }[];
    }
  | {
      type: "section";
      id: string;
      label: string;
      children: { pageId: string }[];
    };

export interface ConsoleConfig {
  schemaVersion: 1;
  catalog: ConsoleCatalog;
  defaultNavLayout?: ConsoleNavLayoutEntry[];
  auth: ConsoleAuthConfig;
}

export type ConsoleConfigSource =
  | { type: "static"; config: ConsoleConfig }
  | { type: "url"; url: string }
  | { type: "factory"; load: () => ConsoleConfig | Promise<ConsoleConfig> };

export interface ConsoleConfigState {
  config: ConsoleConfig | null;
  loading: boolean;
  error: Error | null;
}
