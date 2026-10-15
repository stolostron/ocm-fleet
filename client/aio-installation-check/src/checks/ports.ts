import type { CheckResult } from "../contracts.js";
import { type CheckDependencies, resolveDependencies } from "./dependencies.js";

const AIO_PORTS = [8085, 50051] as const;

export async function checkAioPorts(
  overrides: Partial<CheckDependencies> = {},
): Promise<CheckResult[]> {
  const dependencies = resolveDependencies(overrides);
  return Promise.all(
    AIO_PORTS.map(async (port): Promise<CheckResult> => {
      const id = `aio-port-${port}`;
      const title = `AIO port 127.0.0.1:${port}`;
      try {
        const available = await dependencies.isPortAvailable("127.0.0.1", port);
        if (available) {
          return {
            id,
            title,
            status: "pass",
            reason: `127.0.0.1:${port} is available for AIO.`,
            blocking: false,
          };
        }
        return {
          id,
          title,
          status: "error",
          reason: `127.0.0.1:${port} is already in use.`,
          blocking: true,
          remediation: {
            explanation: `Stop the process using 127.0.0.1:${port}, or free the port before launching AIO.`,
            documentationUrl:
              "https://docs.example.invalid/fleetshift/aio-installation-check#ports",
          },
        };
      } catch (error) {
        return {
          id,
          title,
          status: "error",
          reason: `Could not determine whether 127.0.0.1:${port} is available: ${error instanceof Error ? error.message : String(error)}.`,
          blocking: true,
          remediation: {
            explanation:
              "Resolve the host network probe error and rerun preflight.",
            documentationUrl:
              "https://docs.example.invalid/fleetshift/aio-installation-check#ports",
          },
        };
      }
    }),
  );
}
