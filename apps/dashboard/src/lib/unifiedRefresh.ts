import { postMarketsRefreshFull } from "@/lib/api";

/** Full cache invalidation + blocking Gamma sync (all dashboard refresh buttons). */
export async function runUnifiedMarketsRefresh(): Promise<void> {
  await postMarketsRefreshFull({ wait: true });
}
