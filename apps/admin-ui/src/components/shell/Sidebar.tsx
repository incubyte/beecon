import { Link } from "@tanstack/react-router";
import {
  Activity,
  Boxes,
  Building2,
  ChevronsLeft,
  ChevronsRight,
  FileJson,
  Gauge,
  KeyRound,
  Radio,
  ScrollText,
  ShieldCheck,
  ShieldUser,
  Settings,
  Users,
  Webhook,
  Wrench,
  Zap,
  type LucideIcon,
} from "lucide-react";
import { useState } from "react";

interface NavItem {
  label: string;
  icon: LucideIcon;
  to?:
    | "/organizations"
    | "/connections"
    | "/trigger-instances"
    | "/dashboard"
    | "/logs"
    | "/events"
    | "/users"
    | "/api-keys"
    | "/operators"
    | "/governance"
    | "/providers"
    | "/tools"
    | "/trigger-definitions"
    | "/settings/retention"
    | "/settings/webhook-endpoints"
    | "/settings/config";
}

interface NavGroup {
  label: string;
  items: NavItem[];
}

/** The grouped nav DESIGN.md §5 specifies. "Organizations" (Slice 1),
 * "Connections"/"Trigger Instances" (Slice 2), "Dashboard"/"Logs"/"Events &
 * Delivery" (Slice 3), "Users"/"API Keys" (Slice 4), "Governance" (Slice 5),
 * "Providers"/"Tools"/"Trigger Definitions" (Slice 6), "Settings" (Slice 7,
 * retention config — routes to /settings/retention), "Webhook Endpoints"
 * (Slice 8, multi-endpoint CRUD, filters, auto-disable — routes to
 * /settings/webhook-endpoints), "Config" (Slice 9, export/import — routes to
 * /settings/config), and, as of Phase 5 Slice 4, "Operators" (operator
 * account management — routes to /operators) have a `to` — every other area
 * is real estate for later slices and renders present-but-disabled rather
 * than a dead link. */
const groups: NavGroup[] = [
  {
    label: "Observe",
    items: [
      { label: "Dashboard", icon: Gauge, to: "/dashboard" },
      { label: "Logs", icon: ScrollText, to: "/logs" },
      { label: "Events & Delivery", icon: Webhook, to: "/events" },
      { label: "Metrics", icon: Activity },
    ],
  },
  {
    label: "Operate",
    items: [
      { label: "Connections", icon: Zap, to: "/connections" },
      { label: "Trigger Instances", icon: Radio, to: "/trigger-instances" },
    ],
  },
  {
    label: "Catalog",
    items: [
      { label: "Providers", icon: Boxes, to: "/providers" },
      { label: "Tools", icon: Wrench, to: "/tools" },
      { label: "Trigger Definitions", icon: Radio, to: "/trigger-definitions" },
    ],
  },
  {
    label: "Administer",
    items: [
      { label: "Organizations", icon: Building2, to: "/organizations" },
      { label: "Users", icon: Users, to: "/users" },
      { label: "API Keys", icon: KeyRound, to: "/api-keys" },
      { label: "Operators", icon: ShieldUser, to: "/operators" },
    ],
  },
  {
    label: "Govern",
    items: [
      { label: "Governance", icon: ShieldCheck, to: "/governance" },
      { label: "Settings", icon: Settings, to: "/settings/retention" },
      { label: "Webhook Endpoints", icon: Webhook, to: "/settings/webhook-endpoints" },
      { label: "Config", icon: FileJson, to: "/settings/config" },
    ],
  },
];

export function Sidebar() {
  const [isCollapsed, setIsCollapsed] = useState(false);

  return (
    <aside
      aria-label="Primary"
      className={`flex shrink-0 flex-col border-r border-border bg-surface transition-[width] duration-150 motion-reduce:transition-none ${
        isCollapsed ? "w-14" : "w-62"
      }`}
    >
      <nav className="flex-1 overflow-y-auto px-2 py-4">
        {groups.map((group) => (
          <div key={group.label} className="mb-4">
            {!isCollapsed ? (
              <p className="mb-1 px-2 text-[11px] font-semibold tracking-wider text-text-muted uppercase">
                {group.label}
              </p>
            ) : null}
            <ul className="flex flex-col gap-0.5">
              {group.items.map((item) => (
                <li key={item.label}>
                  <NavLink item={item} collapsed={isCollapsed} />
                </li>
              ))}
            </ul>
          </div>
        ))}
      </nav>

      <button
        type="button"
        onClick={() => setIsCollapsed((value) => !value)}
        aria-label={isCollapsed ? "Expand sidebar" : "Collapse sidebar"}
        className="flex min-h-11 items-center justify-center gap-2 border-t border-border text-text-secondary transition-colors hover:bg-surface-muted hover:text-text cursor-pointer"
      >
        {isCollapsed ? (
          <ChevronsRight className="size-4" aria-hidden="true" />
        ) : (
          <>
            <ChevronsLeft className="size-4" aria-hidden="true" />
            <span className="text-sm">Collapse</span>
          </>
        )}
      </button>
    </aside>
  );
}

function NavLink({ item, collapsed }: { item: NavItem; collapsed: boolean }) {
  const Icon = item.icon;

  if (!item.to) {
    return (
      <span
        aria-disabled="true"
        title="Coming in a later slice"
        className="flex min-h-11 items-center gap-2.5 rounded-md px-2.5 text-sm text-text-muted/70"
      >
        <Icon className="size-4 shrink-0" aria-hidden="true" />
        {!collapsed ? <span>{item.label}</span> : null}
      </span>
    );
  }

  return (
    <Link
      to={item.to}
      search={(prev) => prev}
      className="flex min-h-11 items-center gap-2.5 rounded-md px-2.5 text-sm text-text-secondary transition-colors hover:bg-surface-muted hover:text-text data-[status=active]:bg-primary/10 data-[status=active]:text-primary"
    >
      <Icon className="size-4 shrink-0" aria-hidden="true" />
      {!collapsed ? <span>{item.label}</span> : null}
    </Link>
  );
}
