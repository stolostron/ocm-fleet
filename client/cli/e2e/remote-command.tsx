import { Text } from "ink";
import { useEffect, useState } from "react";

export const remoteLabel = "Node federation command";

export default function RemoteCommand() {
  const [status, setStatus] = useState("loading");

  useEffect(() => {
    setStatus("ready");
  }, []);

  return <Text>Remote command: {status}</Text>;
}
