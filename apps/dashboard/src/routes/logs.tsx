import { createFileRoute, redirect } from "@tanstack/react-router";

export const Route = createFileRoute("/logs")({
  beforeLoad: () => {
    throw redirect({ to: "/monitor", replace: true });
  },
  component: () => null,
});
