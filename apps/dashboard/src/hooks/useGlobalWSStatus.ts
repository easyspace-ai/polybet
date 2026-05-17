import { useEffect, useSyncExternalStore } from "react";
import { riskWsBus, type PolyStatusMessage } from "@/lib/wsBus";
import {
  getAllChannelSnapshots,
  setOBRequired,
  setUSERRequired,
  setUpstreamFromPolyStatus,
  subscribeWSConnectionLog,
} from "@/lib/wsConnectionLog";
import { getOpenRiskPositionCount } from "@/hooks/useRiskControlCache";
import { getWSConfig } from "@/hooks/useWSConfig";
import { getWSStatus, postWSReconnect } from "@/lib/api";

function applyPolyStatus(msg: PolyStatusMessage) {
  setUpstreamFromPolyStatus(msg);
  const openN = getOpenRiskPositionCount();
  setOBRequired(openN > 0, msg.polyOrderbookConnected === true);
  setUSERRequired(true, msg.polyUserConnected === true);
}

export function useGlobalWSStatus() {
  const snapshots = useSyncExternalStore(subscribeWSConnectionLog, getAllChannelSnapshots, getAllChannelSnapshots);

  useEffect(() => {
    return riskWsBus.onPolyStatus((msg) => applyPolyStatus(msg));
  }, []);

  useEffect(() => {
    const cfg = getWSConfig();
    if (!cfg.wsAutoRequestUpstreamReconnect) return;
    const id = setInterval(async () => {
      try {
        const st = await getWSStatus();
        applyPolyStatus({
          type: "poly_status",
          polyOrderbookConnected: st.polyOrderbookConnected,
          polyUserConnected: st.polyUserConnected,
          orderbookNextRetryAt: st.orderbookNextRetryAt,
          orderbookReconnectAttempt: st.orderbookReconnectAttempt,
          userNextRetryAt: st.userNextRetryAt,
          userReconnectAttempt: st.userReconnectAttempt,
          userWsLastIssue: st.userWsLastIssue,
          wsEvents: st.wsEvents,
        });
        const openN = st.openPositionsCount ?? getOpenRiskPositionCount();
        const obRequired = openN > 0;
        if (obRequired && st.polyOrderbookConnected === false) {
          void postWSReconnect("orderbook");
        }
        if (st.polyUserConnected === false) {
          void postWSReconnect("user");
        }
      } catch {
        /* ignore */
      }
    }, cfg.wsRiskPollIntervalSec * 1000);
    return () => clearInterval(id);
  }, []);

  return { channels: snapshots, reconnectRelay: () => riskWsBus.reconnect(true) };
}
