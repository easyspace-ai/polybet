import { createFileRoute, redirect } from "@tanstack/react-router";

export const Route = createFileRoute("/risk")({
  beforeLoad: () => {
    throw redirect({ to: "/monitor", replace: true });
  },
});
