import { runStandalone } from "./flows/runtime";

void runStandalone(process.argv.slice(2)).catch((error: unknown) => {
  console.error(`AIO installation check failed: ${String(error)}`);
  process.exitCode = 1;
});
