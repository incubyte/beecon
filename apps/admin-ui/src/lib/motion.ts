import { useEffect, useState } from "react";

const REDUCED_MOTION_QUERY = "(prefers-reduced-motion: reduce)";

/** usePrefersReducedMotion tracks the `prefers-reduced-motion` media query
 * (DESIGN.md §8/§9): the dashboard's recharts series read this to disable
 * chart-entry animation, mirroring the CSS-level reduced-motion override
 * globals.css already applies to every other transition/animation. */
export function usePrefersReducedMotion(): boolean {
  const [prefersReduced, setPrefersReduced] = useState(readPrefersReducedMotion);

  useEffect(() => {
    if (typeof window === "undefined" || !window.matchMedia) {
      return undefined;
    }
    const mediaQuery = window.matchMedia(REDUCED_MOTION_QUERY);
    const listener = () => setPrefersReduced(mediaQuery.matches);
    mediaQuery.addEventListener("change", listener);
    return () => mediaQuery.removeEventListener("change", listener);
  }, []);

  return prefersReduced;
}

function readPrefersReducedMotion(): boolean {
  if (typeof window === "undefined" || !window.matchMedia) {
    return false;
  }
  return window.matchMedia(REDUCED_MOTION_QUERY).matches;
}
