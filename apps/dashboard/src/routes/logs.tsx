import { createFileRoute, redirect } from "@tanstack/react-router";

export const Route = createFileRoute("/logs")({
  beforeLoad: () => {
    throw redirect({ to: "/risk", replace: true });
  },
  component: () => null,
});
