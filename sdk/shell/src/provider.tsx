import { ScalprumProvider, useScalprum } from "@scalprum/react-core";
import equal from "fast-deep-equal";
import {
  createContext,
  type ReactNode,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";

import { loadConfig } from "./config";
import { transformManifest } from "./manifest";
import type {
  ConsoleCatalog,
  ConsoleConfig,
  ConsoleConfigSource,
  ConsoleConfigState,
} from "./types";

const ConsoleContext = createContext<ConsoleConfigState | null>(null);
const loadingState: ConsoleConfigState = {
  config: null,
  loading: true,
  error: null,
};
const pluginSDKOptions = {
  pluginLoaderOptions: { transformPluginManifest: transformManifest },
};

export function useConsoleConfig(): ConsoleConfigState {
  const state = useContext(ConsoleContext);
  if (!state)
    throw new Error("useConsoleConfig must be used within ConsoleProvider");
  return state;
}

interface ConsoleProviderProps {
  source: ConsoleConfigSource;
  children: ReactNode;
}

export function ConsoleProvider({ source, children }: ConsoleProviderProps) {
  const [loaded, setLoaded] = useState<{
    source: ConsoleConfigSource;
    state: ConsoleConfigState;
  } | null>(null);
  const catalogRef = useRef<ConsoleCatalog | null>(null);
  useEffect(() => {
    const controller = new AbortController();
    let active = true;
    loadConfig(source, controller.signal)
      .then((config) => {
        if (!active) return;
        if (
          catalogRef.current !== null &&
          !equal(catalogRef.current, config.catalog)
        ) {
          setLoaded({
            source,
            state: {
              config: null,
              loading: false,
              error: new Error(
                "Changing the plugin catalog requires a page reload",
              ),
            },
          });
          return;
        }
        catalogRef.current = config.catalog;
        setLoaded({ source, state: { config, loading: true, error: null } });
      })
      .catch((cause: unknown) => {
        if (active)
          setLoaded({
            source,
            state: {
              config: null,
              loading: false,
              error: cause instanceof Error ? cause : new Error(String(cause)),
            },
          });
      });
    return () => {
      active = false;
      controller.abort();
    };
  }, [source]);

  const state = loaded?.source === source ? loaded.state : loadingState;
  if (!state.config)
    return (
      <ConsoleContext.Provider value={state}>
        {children}
      </ConsoleContext.Provider>
    );
  return (
    <ConfiguredConsole config={state.config}>{children}</ConfiguredConsole>
  );
}

function ConfiguredConsole({
  config,
  children,
}: {
  config: ConsoleConfig;
  children: ReactNode;
}) {
  const apps = useMemo(
    () =>
      Object.fromEntries(
        config.catalog.pluginEntries.map(({ key, pluginManifest }) => [
          key,
          { name: pluginManifest.name, pluginManifest },
        ]),
      ),
    [config],
  );
  return (
    <ScalprumProvider config={apps} pluginSDKOptions={pluginSDKOptions}>
      <PluginLoader config={config}>{children}</PluginLoader>
    </ScalprumProvider>
  );
}

function PluginLoader({
  config,
  children,
}: {
  config: ConsoleConfig;
  children: ReactNode;
}) {
  const { pluginStore } = useScalprum();
  const [result, setResult] = useState<{
    config: ConsoleConfig;
    error: Error | null;
  } | null>(null);
  useEffect(() => {
    let active = true;
    const entries = config.catalog.pluginEntries;
    Promise.all(
      entries.map(({ pluginManifest }) =>
        pluginStore.loadPlugin(pluginManifest),
      ),
    )
      .then(() => {
        const names = new Set(
          entries.map(({ pluginManifest }) => pluginManifest.name),
        );
        const failed = pluginStore
          .getPluginInfo()
          .find(
            (plugin) =>
              names.has(plugin.manifest.name) && plugin.status === "failed",
          );
        if (active)
          setResult({
            config,
            error:
              failed && failed.status === "failed"
                ? new Error(
                    `Plugin ${failed.manifest.name} failed: ${failed.errorMessage}`,
                  )
                : null,
          });
      })
      .catch((cause: unknown) => {
        if (active)
          setResult({
            config,
            error: cause instanceof Error ? cause : new Error(String(cause)),
          });
      });
    return () => {
      active = false;
    };
  }, [config, pluginStore]);
  const state: ConsoleConfigState =
    result?.config === config
      ? { config, loading: false, error: result.error }
      : { config, loading: true, error: null };
  return (
    <ConsoleContext.Provider value={state}>{children}</ConsoleContext.Provider>
  );
}
