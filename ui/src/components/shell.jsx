import { Link, NavLink, Outlet } from "react-router-dom";
import {
  Activity,
  Check,
  CircleAlert,
  FolderOpen,
  HardDrive,
  Loader2,
  Monitor,
  Moon,
  Play,
  Sun,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useLive } from "@/lib/live";
import { useTheme } from "@/lib/theme";
import { cn } from "@/lib/utils";

const NAV = [
  { to: "/jobs", label: "Jobs", icon: Play },
  { to: "/repositories", label: "Repositories", icon: HardDrive },
  { to: "/folders", label: "Folders", icon: FolderOpen },
  { to: "/activity", label: "Activity", icon: Activity },
];

function Brand() {
  return (
    <Link
      to="/jobs"
      className="flex shrink-0 items-center gap-2.5 rounded-md py-1 pr-2 transition-opacity hover:opacity-80"
    >
      <span className="grid size-7 shrink-0 place-items-center rounded-md bg-primary text-primary-foreground">
        <svg viewBox="0 0 24 24" className="size-4" fill="none" aria-hidden="true">
          <path
            d="M12 4a8 8 0 1 0 8 8"
            stroke="currentColor"
            strokeWidth="2.5"
            strokeLinecap="round"
          />
          <path d="M12 8v4l3 2" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" />
        </svg>
      </span>
      <span className="display hidden truncate text-[13px] font-medium sm:block">restic</span>
    </Link>
  );
}

/**
 * NavTabs is the app's primary navigation: a single segmented box, so the four
 * sections read as one control rather than four loose links.
 */
function NavTabs({ className, activeCount }) {
  return (
    <nav className={cn("flex items-center gap-0.5 rounded-lg bg-muted p-0.5", className)}>
      {NAV.map(({ to, label, icon: Icon }) => (
        <NavLink
          key={to}
          to={to}
          className={({ isActive }) =>
            cn(
              "flex items-center gap-1.5 whitespace-nowrap rounded-[6px] px-2.5 py-1.5 text-[13px] transition-colors",
              isActive
                // The active tab sits *above* the track in both themes: white on
                // grey in light, a lighter grey on near-black in dark.
                ? "bg-background text-foreground dark:bg-accent"
                : "text-muted-foreground hover:text-foreground",
            )
          }
        >
          <Icon className="size-3.5 shrink-0" />
          {label}
          {label === "Activity" && activeCount > 0 ? (
            <span className="ml-0.5 rounded-full bg-warning/15 px-1.5 text-[10px] tabular text-warning">
              {activeCount}
            </span>
          ) : null}
        </NavLink>
      ))}
    </nav>
  );
}

function ResticStatus() {
  const { status } = useLive();
  const version = (status.resticVersion || "").split("\n")[0];

  if (!status.resticInstalled) {
    return (
      <span className="flex items-center gap-1.5 rounded-full bg-destructive/15 px-2.5 py-1 text-xs font-medium text-destructive">
        <CircleAlert className="size-3.5" />
        <span className="hidden sm:inline">restic not found</span>
      </span>
    );
  }
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span className="flex items-center gap-1.5 rounded-full bg-success/12 px-2.5 py-1 text-xs font-medium text-success">
          <Check className="size-3.5" />
          <span className="hidden lg:inline">restic ready</span>
        </span>
      </TooltipTrigger>
      <TooltipContent>{version || "restic is installed"}</TooltipContent>
    </Tooltip>
  );
}

function ActivityIndicator() {
  const { activeRuns } = useLive();
  if (activeRuns.length === 0) return null;
  return (
    <Button asChild variant="ghost" size="sm" className="gap-1.5 text-warning hover:text-warning">
      <Link to="/activity">
        <Loader2 className="size-3.5 animate-spin" />
        <span className="tabular hidden sm:inline">{activeRuns.length} running</span>
        <span className="tabular sm:hidden">{activeRuns.length}</span>
      </Link>
    </Button>
  );
}

function ThemeToggle() {
  const { theme, setTheme, resolved } = useTheme();
  const Icon = resolved === "dark" ? Moon : Sun;
  const options = [
    { value: "light", label: "Light", icon: Sun },
    { value: "dark", label: "Dark", icon: Moon },
    { value: "system", label: "System", icon: Monitor },
  ];
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon-sm" aria-label="Change theme">
          <Icon className="size-4" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        {options.map(({ value, label, icon: OptIcon }) => (
          <DropdownMenuItem key={value} onSelect={() => setTheme(value)}>
            <OptIcon className="size-4" />
            <span className="flex-1">{label}</span>
            {theme === value ? <Check className="size-3.5 opacity-70" /> : null}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

/** ResticBanner is the one blocking problem worth interrupting for. */
function ResticBanner() {
  const { status } = useLive();
  if (status.resticInstalled) return null;
  return (
    <div className="border-b border-destructive/30 bg-destructive/10">
      <div className="mx-auto w-full max-w-5xl px-5 py-3 text-[13px] sm:px-8">
        <strong className="font-semibold">restic is not installed.</strong> This app runs the{" "}
        <code className="font-mono text-xs">restic</code> binary — install it (for example{" "}
        <code className="font-mono text-xs">brew install restic</code>, or see{" "}
        <a
          href="https://restic.net"
          target="_blank"
          rel="noopener noreferrer"
          className="text-primary underline underline-offset-2"
        >
          restic.net
        </a>
        ) and restart the app.
      </div>
    </div>
  );
}

export function Shell() {
  const { activeRuns } = useLive();
  const activeCount = activeRuns.length;

  return (
    <div className="flex min-h-dvh flex-col bg-background">
      <header className="sticky top-0 z-40 border-b border-border bg-background/85 backdrop-blur">
        <div className="mx-auto flex w-full max-w-5xl items-center gap-3 px-5 py-2.5 sm:px-8">
          <Brand />
          {/* Wide screens carry the nav inline; narrow ones get it on its own row. */}
          <NavTabs className="hidden lg:flex" activeCount={activeCount} />
          <div className="ml-auto flex shrink-0 items-center gap-1.5">
            <ActivityIndicator />
            <ResticStatus />
            <ThemeToggle />
          </div>
        </div>
        <div className="border-t border-border lg:hidden">
          <div className="mx-auto w-full max-w-5xl overflow-x-auto scroll-thin px-5 py-2 sm:px-8">
            <NavTabs className="w-max" activeCount={activeCount} />
          </div>
        </div>
      </header>

      <ResticBanner />

      <main className="flex-1">
        <Outlet />
      </main>

      <footer className="border-t border-border">
        <div className="mx-auto w-full max-w-5xl px-5 py-4 text-xs text-muted-foreground sm:px-8">
          Single-user local tool · no authentication · the backup engine is{" "}
          <code className="font-mono">restic</code>
        </div>
      </footer>
    </div>
  );
}
