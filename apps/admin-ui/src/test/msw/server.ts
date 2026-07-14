import { setupServer } from "msw/node";

import { handlers } from "./handlers";

/**
 * The shared MSW server every test file runs against (§2.9): started once
 * in vitest.setup.ts, handlers reset after each test so one test's
 * `server.use(...)` override never leaks into the next.
 */
export const server = setupServer(...handlers);
