// Perry 0.5.1520's readline thread queues bytes without notifying the event
// pump. Its idle wait is 1000 ms. A prompt-scoped deadline bounds dispatch
// latency; this is not a spinner and does no rendering. Node needs no polling.
export function inputWake(): () => void {
  if (!process.versions["perry"]) return () => {};
  const timer = setInterval(() => {}, 16);
  return () => clearInterval(timer);
}
