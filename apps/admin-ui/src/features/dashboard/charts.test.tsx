import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ConnectionsByStatusChart, DeliveryOutcomesChart } from "./charts";

/** stubMatchMedia mirrors lib/motion.test.ts's own fake — a controllable
 * MediaQueryList so a test can render under a chosen prefers-reduced-motion
 * value without depending on the real OS setting. */
function stubMatchMedia(matches: boolean) {
  window.matchMedia = vi.fn().mockImplementation((query: string) => ({
    matches,
    media: query,
    onchange: null,
    addListener: vi.fn(),
    removeListener: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    dispatchEvent: vi.fn(),
  }));
}

describe("ConnectionsByStatusChart", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("labels every bar by its own status name as text (never color alone)", () => {
    render(<ConnectionsByStatusChart connectionsByStatus={{ ACTIVE: 3, INITIATED: 1, EXPIRED: 0, DISCONNECTED: 2 }} />);

    for (const label of ["ACTIVE", "INITIATED", "EXPIRED", "DISCONNECTED"]) {
      expect(screen.getByText(label)).toBeInTheDocument();
    }
  });

  // usePrefersReducedMotion feeds `isAnimationActive={!prefersReducedMotion}`
  // directly on the recharts Bar — recharts' own animation timing isn't
  // reliably observable through jsdom (its Animate primitive doesn't settle
  // synchronously either way), so `data-chart-animation` is a small,
  // production-minimal testability seam on the chart's own wrapper div,
  // carrying the exact same boolean the Bar prop is derived from.
  it("data-chart-animation is 'disabled' when the OS prefers reduced motion", () => {
    stubMatchMedia(true);
    const { container } = render(<ConnectionsByStatusChart connectionsByStatus={{}} />);

    expect(container.firstChild).toHaveAttribute("data-chart-animation", "disabled");
  });

  it("data-chart-animation is 'enabled' when the OS has no reduced-motion preference", () => {
    stubMatchMedia(false);
    const { container } = render(<ConnectionsByStatusChart connectionsByStatus={{}} />);

    expect(container.firstChild).toHaveAttribute("data-chart-animation", "enabled");
  });
});

describe("DeliveryOutcomesChart", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("labels each result series with a text legend name (Success/Failure), not color alone", () => {
    render(
      <DeliveryOutcomesChart
        deliveryOutcomes={[
          { type: "trigger.event", result: "success", count: 4 },
          { type: "trigger.event", result: "failure", count: 1 },
        ]}
      />,
    );

    expect(screen.getByText("Success")).toBeInTheDocument();
    expect(screen.getByText("Failure")).toBeInTheDocument();
    expect(screen.getByText("trigger.event")).toBeInTheDocument();
  });

  it("shows a plain-text empty state instead of an empty chart when there are no delivery attempts", () => {
    render(<DeliveryOutcomesChart deliveryOutcomes={[]} />);

    expect(screen.getByText(/no delivery attempts recorded yet/i)).toBeInTheDocument();
  });

  it("data-chart-animation is 'disabled' when the OS prefers reduced motion", () => {
    stubMatchMedia(true);
    const { container } = render(
      <DeliveryOutcomesChart deliveryOutcomes={[{ type: "trigger.event", result: "success", count: 1 }]} />,
    );

    expect(container.firstChild).toHaveAttribute("data-chart-animation", "disabled");
  });
});
