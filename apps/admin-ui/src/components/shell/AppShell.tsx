import type { ReactNode } from "react";

import { ReauthModal } from "@/components/ReauthModal";

import { Sidebar } from "./Sidebar";
import { TopBar } from "./TopBar";

export interface AppShellProps {
  children: ReactNode;
}

/** AppShell is the authenticated console layout (Slice 1, AC6): the left
 * sidebar, the slim top bar, and the routed page content. ReauthModal (Slice
 * 5) is mounted here, always, so a mid-session 401 from any page's own API
 * call can surface it as an overlay without unmounting anything underneath —
 * it renders nothing (Radix's own Portal stays empty) whenever
 * useReauthRequired() is false. */
export function AppShell({ children }: AppShellProps) {
  return (
    <div className="flex h-screen flex-col">
      <TopBar />
      <div className="flex min-h-0 flex-1">
        <Sidebar />
        <main className="min-w-0 flex-1 overflow-y-auto bg-bg p-6">{children}</main>
      </div>
      <ReauthModal />
    </div>
  );
}
