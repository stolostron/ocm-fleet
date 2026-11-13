import type { PluginManifest } from "@openshift/dynamic-plugin-sdk";

/** Scalprum's default transform prefixes absolute scripts too. Resolve per manifest instead. */
export function transformManifest<T extends PluginManifest>(manifest: T): T {
  if (!("loadScripts" in manifest)) return manifest;
  const base = new URL(
    manifest.baseURL.endsWith("/") ? manifest.baseURL : `${manifest.baseURL}/`,
    document.baseURI,
  );
  return {
    ...manifest,
    loadScripts: manifest.loadScripts.map((script) => {
      if (/^(?:[a-z][a-z\d+.-]*:|\/\/)/i.test(script)) return script;
      return new URL(script.replace(/^\/+/, ""), base).href;
    }),
  };
}
