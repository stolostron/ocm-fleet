import { Box, Text } from "ink";
import Table from "ink-table";
import React from "react";

import { flagString } from "../../argv";
import { clientForArgs } from "../context";
import type { CommandSpec } from "../types";
import type { DeploymentListResponse } from "./types";

export const listCommand = {
  path: "deployment list",
  description: "List deployments",
  implemented: true,
  present: (data) => <DeploymentListView data={data} />,
  run: async ({ args }) => {
    const query = new URLSearchParams();
    const pageSize = flagString(args, "page-size");
    if (pageSize) query.set("pageSize", pageSize);
    const response = await (
      await clientForArgs(args)
    ).request<DeploymentListResponse>(
      `/v1alpha/deployments${query.size ? `?${query}` : ""}`,
      {
        method: "GET",
      },
    );
    return response.deployments ?? [];
  },
} satisfies CommandSpec<NonNullable<DeploymentListResponse["deployments"]>>;

export function DeploymentListView({
  data: deployments,
}: {
  data: NonNullable<DeploymentListResponse["deployments"]>;
}): React.ReactElement {
  const tableData = deployments.map((deployment) => ({
    name: deployment.name,
    state: deployment.state ?? "UNKNOWN",
    reconciling: Boolean(deployment.reconciling),
    createTime: deployment.createTime ?? "UNKNOWN",
    updated: deployment.updateTime ?? "UNKNOWN",
    targets: (deployment.resolvedTargetIds ?? ["NONE"]).join(", "),
    pauseReason: deployment.pauseReason ?? "NONE",
  }));
  return (
    <Box flexDirection="column">
      <Table data={tableData} />
      <Text dimColor>Use --output json or yaml for full details.</Text>
    </Box>
  );
}
