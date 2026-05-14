import { useState, useEffect } from "react";
import { wsBus, type PolyStatusMessage } from "@/lib/wsBus";

export interface WsStatus {
  dashConnected: boolean;
  polyOrderbookConnected: boolean;
  polyUserConnected: boolean;
  allConnected: boolean;
}

export function useWsStatus() {
  const [dashConnected, setDashConnected] = useState(false);
  const [polyStatus, setPolyStatus] = useState<PolyStatusMessage | null>(null);

  useEffect(() => {
    return wsBus.onStatusChange(setDashConnected);
  }, []);

  useEffect(() => {
    const unsub = wsBus.onPolyStatus((msg) => {
      setPolyStatus(msg);
    });
    return unsub;
  }, []);

  const polyOrderbookConnected = polyStatus?.polyOrderbookConnected ?? true;
  const polyUserConnected = polyStatus?.polyUserConnected ?? true;

  return {
    dashConnected,
    polyOrderbookConnected,
    polyUserConnected,
    allConnected: dashConnected && polyOrderbookConnected && polyUserConnected,
  } satisfies WsStatus;
}
