import { createFileRoute, redirect } from "@tanstack/react-router";

/** The dashboard (Slice 3) is now the default post-login landing —
 * Organizations was only a stand-in until this slice built the real one
 * (architecture doc §8, Slice 3 scope note). */
export const Route = createFileRoute("/")({
  beforeLoad: () => {
    throw redirect({ to: "/dashboard" });
  },
});
