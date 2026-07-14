import type { ReactNode } from "react";

import { Sidebar } from "./Sidebar";
import { TopBar } from "./TopBar";

export interface AppShellProps {
  children: ReactNode;
}

/** AppShell is the authenticated console layout (Slice 1, AC6): the left
 * sidebar, the slim top bar, and the routed page content. */
export function AppShell({ children }: AppShellProps) {
  return (
    <div className="flex h-screen flex-col">
      <TopBar />
      <div className="flex min-h-0 flex-1">
        <Sidebar />
        <main className="min-w-0 flex-1 overflow-y-auto bg-bg p-6">{children}</main>
      </div>
    </div>
  );
}
