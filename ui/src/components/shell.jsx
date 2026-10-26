import * as React from "react";
import { Link, NavLink, Outlet, useLocation, useNavigate } from "react-router-dom";
import {
  Activity,
  CircleAlert,
  Container,
  FolderOpen,
  LayoutGrid,
  Loader2,
  LogOut,
  Menu,
  Monitor,
  Moon,
  Settings,
  Sun,
  Vault,
  Workflow,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Sheet, SheetContent, SheetTitle } from "@/components/ui/sheet";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { TONE_TEXT } from "@/components/kind";
import { useAuth } from "@/lib/auth";
import { useDockerStatus } from "@/lib/docker";
import { useLive } from "@/lib/live";
import { useTheme } from "@/lib/theme";
import { cn } from "@/lib/utils";

// Each section's icon takes its kind's hue when active (see components/kind.jsx).
const NAV = [
  { to: "/dashboard", label: "Dashboard", icon: LayoutGrid, tone: "brand" },
  { to: "/jobs", label: "Jobs", icon: Workflow, tone: "violet" },
  { to: "/repositories", label: "Repositories", icon: Vault, tone: "cyan" },
  { to: "/folders", label: "Sources", icon: FolderOpen, tone: "fuchsia" },
  { to: "/activity", label: "Activity", icon: Activity, tone: "brand" },
];

/** The sites tab appears once Docker answers: named for Coolify when Coolify
 * runs the host, plain Docker otherwise. */
function useNav() {
  const docker = useDockerStatus();
  if (!docker?.available) return NAV;
  const sites = {
    to: "/coolify",
    label: docker.coolify ? "Coolify" : "Docker",
    icon: Container,
    tone: "fuchsia",
  };
  return [...NAV.slice(0, 2), sites, ...NAV.slice(2)];
}

export function BrandMark({ className }) {
  return (
    <span
      className={cn(
        "grid size-9 shrink-0 place-items-center rounded-[11px] bg-primary text-primary-foreground",
        className,
      )}
    >
      <svg viewBox="0 0 24 24" className="size-[18px]" fill="none" aria-hidden="true">
        <path d="M12 4a8 8 0 1 0 8 8" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" />
        <path d="M12 8v4l3 2" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" />
      </svg>
    </span>
  );
}

function Brand({ onNavigate }) {
  return (
    <Link
      to="/dashboard"
      onClick={onNavigate}
      className="flex min-w-0 items-center gap-3 rounded-xl transition-opacity hover:opacity-85"
    >
      <BrandMark />
      <span className="min-w-0 leading-tight">
        <span className="display block text-md font-semibold">restic</span>
        <span className="block truncate text-xs text-muted-foreground">backup manager</span>
      </span>
    </Link>
  );
}

function NavItem({ to, label, icon: Icon, tone, badge, onNavigate }) {
  return (
    <NavLink
      to={to}
      onClick={onNavigate}
      className={({ isActive }) =>
        cn(
          "group flex h-11 items-center gap-3 rounded-xl px-3 text-base font-medium transition-colors",
          isActive
            ? "bg-card text-foreground shadow-[0_1px_2px_rgb(0_0_0/0.05)] ring-1 ring-border"
            : "text-muted-foreground hover:bg-accent hover:text-foreground",
        )
      }
    >
      {({ isActive }) => (
        <>
          <Icon
            className={cn(
              "size-[18px] shrink-0 transition-colors",
              isActive ? TONE_TEXT[tone] : "text-muted-foreground group-hover:text-foreground",
            )}
          />
          <span className="flex-1 truncate">{label}</span>
          {badge}
        </>
      )}
    </NavLink>
  );
}

function RunningCount({ count }) {
  if (!count) return null;
  return (
    <span className="flex items-center gap-1 rounded-full bg-running/12 px-2 py-0.5 text-xs font-semibold tabular text-running">
      <Loader2 className="size-3 animate-spin" />
      {count}
    </span>
  );
}

function ResticStatus() {
  const { status } = useLive();
  const version = (status.resticVersion || "").split("\n")[0];
  const short = (version.match(/restic\s+([\d.]+)/) || [])[1];

  if (!status.resticInstalled) {
    return (
      <div className="flex items-center gap-2.5 rounded-xl bg-destructive/10 px-3 py-2.5 text-sm font-medium text-destructive">
        <CircleAlert className="size-4 shrink-0" />
        restic not found
      </div>
    );
  }
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <div className="flex items-center gap-2.5 rounded-xl px-3 py-2 text-sm text-muted-foreground">
          <span className="relative flex size-2">
            <span className="absolute inline-flex size-full rounded-full bg-success opacity-30" />
            <span className="relative m-auto inline-flex size-2 rounded-full bg-success" />
          </span>
          <span className="truncate">
            restic {short ? <span className="tabular">{short}</span> : null} ready
          </span>
        </div>
      </TooltipTrigger>
      <TooltipContent side="right">{version || "restic is installed"}</TooltipContent>
    </Tooltip>
  );
}

const THEMES = [
  { value: "light", label: "Light", icon: Sun },
  { value: "dark", label: "Dark", icon: Moon },
  { value: "system", label: "System", icon: Monitor },
];

/** ThemeSwitch is a three-way segmented control: light, dark, or follow the system. */
export function ThemeSwitch({ className }) {
  const { theme, setTheme } = useTheme();
  return (
    <div
      role="radiogroup"
      aria-label="Theme"
      className={cn("flex items-center gap-0.5 rounded-[11px] bg-secondary p-1", className)}
    >
      {THEMES.map(({ value, label, icon: Icon }) => (
        <Tooltip key={value}>
          <TooltipTrigger asChild>
            <button
              type="button"
              role="radio"
              aria-checked={theme === value}
              aria-label={`${label} theme`}
              onClick={() => setTheme(value)}
              className={cn(
                "grid h-8 flex-1 place-items-center rounded-lg px-2.5 transition-colors",
                theme === value
                  ? "bg-card text-foreground shadow-[0_1px_2px_rgb(0_0_0/0.08)]"
                  : "text-muted-foreground hover:text-foreground",
              )}
            >
              <Icon className="size-4" />
            </button>
          </TooltipTrigger>
          <TooltipContent>{label}</TooltipContent>
        </Tooltip>
      ))}
    </div>
  );
}

function SidebarContent({ onNavigate }) {
  const nav = useNav();
  const { activeRuns } = useLive();
  const { logout } = useAuth();
  const navigate = useNavigate();

  return (
    <div className="flex h-full flex-col">
      <div className="px-5 pb-6 pt-6">
        <Brand onNavigate={onNavigate} />
      </div>

      <nav className="flex-1 space-y-1 overflow-y-auto scroll-thin px-3" aria-label="Main">
        {nav.map((item) => (
          <NavItem
            key={item.to}
            {...item}
            onNavigate={onNavigate}
            badge={item.to === "/activity" ? <RunningCount count={activeRuns.length} /> : null}
          />
        ))}
      </nav>

      <div className="space-y-3 border-t border-border px-3 pb-4 pt-3">
        <NavItem
          to="/settings"
          label="Settings"
          icon={Settings}
          tone="neutral"
          onNavigate={onNavigate}
        />
        <ResticStatus />
        <div className="flex items-center gap-2 px-1">
          <ThemeSwitch className="flex-1" />
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="icon"
                aria-label="Log out"
                onClick={async () => {
                  onNavigate?.();
                  await logout();
                  navigate("/login", { replace: true });
                }}
              >
                <LogOut />
              </Button>
            </TooltipTrigger>
            <TooltipContent>Log out</TooltipContent>
          </Tooltip>
        </div>
      </div>
    </div>
  );
}

/** On small screens the running count rides in the top bar, one tap from Activity. */
function MobileActivity() {
  const { activeRuns } = useLive();
  if (activeRuns.length === 0) return null;
  return (
    <Link
      to="/activity"
      className="flex h-9 items-center gap-1.5 rounded-full bg-running/12 px-3 text-sm font-semibold text-running"
    >
      <Loader2 className="size-4 animate-spin" />
      <span className="tabular">{activeRuns.length} running</span>
    </Link>
  );
}

/** ResticBanner is the one blocking problem worth interrupting for. */
function ResticBanner() {
  const { status } = useLive();
  if (status.resticInstalled) return null;
  return (
    <div className="border-b border-destructive/25 bg-destructive/8">
      <div className="mx-auto flex w-full max-w-[1180px] items-start gap-3 px-5 py-3.5 text-sm sm:px-8 xl:px-12">
        <CircleAlert className="mt-0.5 size-[18px] shrink-0 text-destructive" />
        <p>
          <strong className="font-semibold">restic is not installed.</strong> This app runs the{" "}
          <code className="font-mono text-[0.8125rem]">restic</code> binary — install it (for
          example <code className="font-mono text-[0.8125rem]">brew install restic</code>, or see{" "}
          <a
            href="https://restic.net"
            target="_blank"
            rel="noopener noreferrer"
            className="font-medium text-primary underline underline-offset-2"
          >
            restic.net
          </a>
          ) and restart the app.
        </p>
      </div>
    </div>
  );
}

export function Shell() {
  const [menuOpen, setMenuOpen] = React.useState(false);
  const location = useLocation();

  // Any navigation closes the drawer, including back/forward.
  React.useEffect(() => {
    setMenuOpen(false);
  }, [location.pathname]);

  return (
    <div className="min-h-dvh bg-background lg:pl-[17rem]">
      <aside className="fixed inset-y-0 left-0 z-30 hidden w-[17rem] border-r border-border bg-sidebar lg:block">
        <SidebarContent />
      </aside>

      <header className="sticky top-0 z-40 border-b border-border bg-background/85 backdrop-blur-md lg:hidden">
        <div className="flex h-16 items-center gap-3 px-4 sm:px-6">
          <Button
            variant="ghost"
            size="icon"
            aria-label="Open menu"
            onClick={() => setMenuOpen(true)}
            className="-ml-1"
          >
            <Menu />
          </Button>
          <Brand />
          <div className="ml-auto">
            <MobileActivity />
          </div>
        </div>
      </header>

      <Sheet open={menuOpen} onOpenChange={setMenuOpen}>
        <SheetContent>
          <SheetTitle className="sr-only">Menu</SheetTitle>
          <SidebarContent onNavigate={() => setMenuOpen(false)} />
        </SheetContent>
      </Sheet>

      <ResticBanner />

      <main>
        <Outlet />
      </main>

      <footer className="mx-auto w-full max-w-[1180px] px-5 pb-8 text-xs text-muted-foreground sm:px-8 xl:px-12">
        Single-user tool · password-protected · the backup engine is{" "}
        <code className="font-mono">restic</code>
      </footer>
    </div>
  );
}
