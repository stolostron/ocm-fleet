// Keep both calls on process.stdin: Perry lowers this exact expression to its
// readline backend. An aliased TTY method uses a different termios snapshot.
export function acquireRawMode(): () => void {
  const wasRaw = process.stdin.isRaw === true;
  try {
    process.stdin.setRawMode(true);
  } catch (error) {
    process.stdin.setRawMode(wasRaw);
    throw error;
  }
  let restored = false;
  return () => {
    if (restored) return;
    restored = true;
    process.stdin.setRawMode(wasRaw);
  };
}
