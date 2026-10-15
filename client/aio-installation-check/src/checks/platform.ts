import type { CheckResult } from "../contracts.js";

const SUPPORTED_PLATFORMS = new Set(["darwin", "linux"]);

export function checkHostPlatform(
  platform: string = process.platform,
): CheckResult {
  if (SUPPORTED_PLATFORMS.has(platform)) {
    const displayName = platform === "darwin" ? "macOS" : "Linux";
    return {
      id: "host-platform",
      title: "Host platform",
      status: "pass",
      reason: `${displayName} is supported for AIO preflight.`,
      blocking: false,
    };
  }

  return {
    id: "host-platform",
    title: "Host platform",
    status: "error",
    reason: `This host platform (${platform}) is unsupported. AIO preflight supports macOS and Linux.`,
    blocking: true,
    remediation: {
      explanation:
        "Run the preflight utility on a supported macOS or Linux host.",
      documentationUrl:
        "https://docs.example.invalid/fleetshift/aio-installation-check",
    },
  };
}
