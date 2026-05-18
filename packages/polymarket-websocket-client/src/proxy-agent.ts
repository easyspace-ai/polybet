/** Node-only proxy agent loader (not used in browser bundles). */
export async function createProxyAgent(
  proxyUrl: string,
  proxyHeaders?: Record<string, string>,
): Promise<unknown> {
  const { HttpsProxyAgent } = await import('https-proxy-agent');
  const agentOptions = proxyHeaders ? { headers: proxyHeaders } : undefined;
  return new HttpsProxyAgent(proxyUrl, agentOptions);
}
