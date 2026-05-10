import { fetch, ProxyAgent } from "undici";

/** Same host/path family as Go `sync/gamma_http.go` (minimal page for probe). */
const GAMMA_PROBE_URL =
  "https://gamma-api.polymarket.com/events?active=true&closed=false&limit=1&offset=0&series_id=10345";

/**
 * Verifies outbound access to Polymarket Gamma (HTTPS GET). Uses HTTP(S)
 * CONNECT proxy when `proxyUrl` is set — same class as Go's HTTP_PLATFORM_PROXY_URL.
 */
export async function probeGammaApiReachable(
  proxyUrl: string | undefined,
  timeoutMs = 20000,
): Promise<{ ok: true } | { ok: false; error: string }> {
  const signal = AbortSignal.timeout(timeoutMs);
  const proxy = proxyUrl?.trim();
  try {
    const res = await fetch(GAMMA_PROBE_URL, {
      signal,
      ...(proxy ? { dispatcher: new ProxyAgent(proxy) } : {}),
    });
    if (!res.ok) {
      return {
        ok: false,
        error: `Gamma API HTTP ${res.status} ${res.statusText}`,
      };
    }
    return { ok: true };
  } catch (e) {
    const msg = e instanceof Error ? e.message : String(e);
    return { ok: false, error: msg };
  }
}
