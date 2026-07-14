import type { Config } from "tailwindcss";

// Tailwind v4 is CSS-first (see src/styles/globals.css's `@theme` block,
// which is where `bg-primary`/`text-text-secondary`/`rounded-lg`/etc.
// actually resolve to the DESIGN.md tokens defined in src/styles/tokens.css)
// — this file exists only so editor tooling (the Tailwind CSS IntelliSense
// extension) can resolve the project's content globs and theme without a
// running dev server. It carries no build-time effect of its own.
export default {
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
} satisfies Config;
