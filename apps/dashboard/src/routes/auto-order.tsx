import { createFileRoute } from "@tanstack/react-router";
import { AutoOrderPage } from "@/components/AutoOrderPage";

export const Route = createFileRoute("/auto-order")({
  component: AutoOrderPage,
});
