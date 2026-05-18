import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { RouterProvider } from "@tanstack/react-router";
import { getRouter } from "./router";

import "./styles.css";
import "@/hooks/useGlobalWSStatus";
import { installMonitorCacheBootstrap } from "@/hooks/useMonitorCache";
import { installMonitorCoordinator } from "@/lib/monitor/coordinator";

installMonitorCoordinator();
installMonitorCacheBootstrap();

const root = document.getElementById("root");

if (!root) {
  throw new Error("Missing root element");
}

createRoot(root).render(
  <StrictMode>
    <RouterProvider router={getRouter()} />
  </StrictMode>,
);
