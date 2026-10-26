import assert from "node:assert/strict";

import { createInstance } from "@module-federation/enhanced/runtime";
import nodeRuntimePlugin from "@module-federation/node/runtimePlugin";

const host = createInstance({
  name: "fixture_host",
  remotes: [],
  plugins: [nodeRuntimePlugin()],
});
host.registerRemotes([
  {
    name: "fixture_remote",
    entry: process.env.FIXTURE_REMOTE_ENTRY,
    type: "commonjs-module",
  },
]);

const exposedModule = await host.loadRemote("fixture_remote/answer");
assert.ok(exposedModule);
assert.equal(exposedModule.answer, 42);
assert.equal(exposedModule.default(), "answer");
assert.equal(await exposedModule.loadLazyAnswer(), "loaded lazily");
console.log("Enhanced HTTP host passed");
