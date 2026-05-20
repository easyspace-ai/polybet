export function abbreviateAddress(addr: string, head = 4, tail = 4): string {
  const a = addr.trim();
  if (a.length <= head + tail + 3) return a;
  return `${a.slice(0, head + 2)}…${a.slice(-tail)}`;
}
