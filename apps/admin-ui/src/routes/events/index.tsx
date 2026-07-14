import { createFileRoute } from "@tanstack/react-router";

import { EventsPage } from "@/features/events/EventsPage";

export const Route = createFileRoute("/events/")({
  component: EventsPage,
});
