import {
  Extension,
  ExtensionPredicate,
  useExtensions,
  useResolvedExtensions,
} from "@openshift/dynamic-plugin-sdk";
import { AppMetadata, AppsConfig, createSharedStore } from "@scalprum/core";
import { useGetState, useScalprum } from "@scalprum/react-core";
import { useEffect, useMemo } from "react";

import { makeRequest } from "../api";

type PluginName = string;

// create map for easy lookup
type DynamicExtensionMap = {
  [extensionId: string]: { plugins: PluginName[]; extension: Extension };
};

function createDynamicExtensionMap(apps: AppsConfig): DynamicExtensionMap {
  return Object.values(apps || {}).reduce<DynamicExtensionMap>((acc, app) => {
    if (app && app.pluginManifest?.extensions) {
      app.pluginManifest.extensions.forEach((extension) => {
        if (!acc[extension.type]) {
          acc[extension.type] = { plugins: [], extension };
        }
        acc[extension.type].plugins.push(app.name);
      });
    }
    return acc;
  }, {});
}

async function fetchRemoteManifests(manifestUrl: string): Promise<AppMetadata> {
  const result = await makeRequest<AppMetadata>(manifestUrl);

  return result;
}

async function embedRemoteManifest(
  appsConfig: AppsConfig,
): Promise<AppsConfig> {
  const updatedAppsConfig: AppsConfig = { ...appsConfig };
  for (const appName in updatedAppsConfig) {
    const app = updatedAppsConfig[appName];
    if (app && app.manifestLocation && !app.pluginManifest) {
      const remoteManifest = await fetchRemoteManifests(app.manifestLocation);
      updatedAppsConfig[appName] = remoteManifest;
    }
  }
  return updatedAppsConfig;
}

const EVENTS = ["UPDATE_STORE"] as const;

const dynamicExtensionStore = createSharedStore({
  initialState: {} as DynamicExtensionMap,
  events: EVENTS,
  onEventChange(prevState, event, payload) {
    if (event === "UPDATE_STORE" && payload) {
      return { ...prevState, ...payload };
    }
    return prevState;
  },
});

function useDynamicExtension<T extends Extension>(
  predicate: ExtensionPredicate<T>,
) {
  const { config, pluginStore } = useScalprum();
  const dynamicExtensionMap = useGetState(dynamicExtensionStore);

  async function handleRemoteManifests(config: AppsConfig = {}) {
    const updatedConfig = await embedRemoteManifest(config);
    dynamicExtensionStore.updateState(
      "UPDATE_STORE",
      createDynamicExtensionMap(updatedConfig),
    );
  }
  useMemo(() => {
    handleRemoteManifests(config);
  }, [config]);

  const validPlugins = useMemo(() => {
    const plugins = Object.entries(dynamicExtensionMap)
      .filter(([, { extension }]) => {
        return predicate(extension);
      })
      .flatMap(([, { plugins }]) => plugins);
    return Array.from(new Set(plugins));
  }, [dynamicExtensionMap, predicate]);

  useEffect(() => {
    for (const plugin of validPlugins) {
      if (config[plugin].pluginManifest) {
        console.log(`Loading plugin via dynamic: ${plugin}`);
        pluginStore.loadPlugin(config[plugin].pluginManifest);
      }
    }
  }, [config, validPlugins, pluginStore]);

  const extensionsCandidates = useExtensions(predicate);
  const extensions = useResolvedExtensions(extensionsCandidates);
  return extensions;
}

export default useDynamicExtension;
