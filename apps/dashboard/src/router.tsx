import { QueryClient } from "@tanstack/react-query";
import { createRouter, createBrowserHistory } from "@tanstack/react-router";
import { routeTree } from "./routeTree.gen";

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30 * 1000,
      refetchOnWindowFocus: false,
    },
  },
});

const browserHistory = createBrowserHistory();

export const router = createRouter({
  routeTree,
  context: { queryClient },
  history: browserHistory,
  defaultPreloadStaleTime: 0,
});

export function getRouter() {
  return router;
}