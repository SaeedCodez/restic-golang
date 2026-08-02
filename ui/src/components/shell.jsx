import * as React from "react";
import { Link, NavLink, Outlet, useLocation } from "react-router-dom";
import * as DialogPrimitive from "@radix-ui/react-dialog";
import {
  Activity,
  Check,
  CircleAlert,
  FolderOpen,
  HardDrive,
  Loader2,
  Menu,
  Monitor,
  Moon,
  Play,
  Sun,
  X,
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

function Brand({ onNavigate }) {
  return (
    <Link
      to="/jobs"
      onClick={onNavigate}
      className="flex items-center gap-2.5 rounded-md px-2 py-1.5 transition-colors hover:bg-accent"
    >
      <span className="grid size-8 shrink-0 place-items-center rounded-lg bg-primary text-primary-foreground">
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
      <span className="min-w-0">
        <span className="block truncate text-[13px] font-semibold leading-tight">
          restic backup
        </span>
        <span className="block truncate text-[11px] leading-tight text-muted-foreground">
          manager
        </span>
      </span>
    </Link>
  );
}

function NavItems({ onNavigate, activeCount }) {
  return (
    <nav className="flex flex-col gap-0.5">
      {NAV.map(({ to, label, icon: Icon }) => (
        <NavLink
          key={to}
          to={to}
          onClick={onNavigate}
          className={({ isActive }) =>
            cn(
              "flex items-center gap-2.5 rounded-md px-2.5 py-2 text-[13px] font-medium transition-colors",
              isActive
                ? "bg-accent text-accent-foreground"
                : "text-muted-foreground hover:bg-accent/60 hover:text-foreground",
            )
          }
        >
          <Icon className="size-4 shrink-0" />
          <span className="flex-1 truncate">{label}</span>
          {label === "Activity" && activeCount > 0 ? (
            <span className="rounded-full bg-warning/20 px-1.5 py-0.5 text-[10px] font-semibold tabular text-warning">
              {activeCount}
            </span>
          ) : null}
        </NavLink>
      ))}
    </nav>
  );
}

function ResticStatus({ compact }) {
  const { status } = useLive();
  const version = (status.resticVersion || "").split("\n")[0];
  if (!status.resticInstalled) {
    return (
      <Link
        to="/repositories"
        className="flex items-center gap-1.5 rounded-full bg-destructive/15 px-2.5 py-1 text-xs font-medium text-destructive"
      >
        <CircleAlert className="size-3.5" />
        restic not found
      </Link>
    );
  }
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span
          className={cn(
            "flex items-center gap-1.5 rounded-full bg-success/12 px-2.5 py-1 text-xs font-medium text-success",
            compact && "px-2",
          )}
        >
          <Check className="size-3.5" />
          <span className={cn(compact && "sr-only")}>restic ready</span>
        </span>
      </TooltipTrigger>
      <TooltipContent>{version || "restic is installed"}</TooltipContent>
    </Tooltip>
  );
}

function ActivityIndicator() {
  const { activeRuns } = useLive();
  const n = activeRuns.length;
  if (n === 0) return null;
  return (
    <Button asChild variant="ghost" size="sm" className="gap-1.5 text-warning hover:text-warning">
      <Link to="/activity">
        <Loader2 className="size-3.5 animate-spin" />
        <span className="tabular">
          {n} running
        </span>
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

function MobileNav({ activeCount }) {
  const [open, setOpen] = React.useState(false);
  const location = useLocation();
  React.useEffect(() => setOpen(false), [location.pathname]);

  return (
    <DialogPrimitive.Root open={open} onOpenChange={setOpen}>
      <DialogPrimitive.Trigger asChild>
        <Button variant="ghost" size="icon-sm" className="lg:hidden" aria-label="Open navigation">
          <Menu className="size-4" />
        </Button>
      </DialogPrimitive.Trigger>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay className="fixed inset-0 z-50 bg-black/70 data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=open]:fade-in-0 data-[state=closed]:fade-out-0" />
        <DialogPrimitive.Content
          className={cn(
            "fixed inset-y-0 left-0 z-50 flex w-64 flex-col gap-4 border-r border-border bg-card p-3",
            "data-[state=open]:animate-in data-[state=closed]:animate-out",
            "data-[state=open]:slide-in-from-left data-[state=closed]:slide-out-to-left",
          )}
        >
          <DialogPrimitive.Title className="sr-only">Navigation</DialogPrimitive.Title>
          <div className="flex items-center justify-between">
            <Brand onNavigate={() => setOpen(false)} />
            <DialogPrimitive.Close asChild>
              <Button variant="ghost" size="icon-sm" aria-label="Close navigation">
                <X className="size-4" />
              </Button>
            </DialogPrimitive.Close>
          </div>
          <NavItems onNavigate={() => setOpen(false)} activeCount={activeCount} />
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  );
}

/** ResticBanner is the one blocking problem worth interrupting for. */
function ResticBanner() {
  const { status } = useLive();
  if (status.resticInstalled) return null;
  return (
    <div className="border-b border-destructive/30 bg-destructive/10 px-5 py-3 text-[13px] sm:px-8">
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
  );
}

export function Shell() {
  const { activeRuns } = useLive();
  const activeCount = activeRuns.length;

  return (
    <div className="flex min-h-dvh bg-background">
      <aside className="sticky top-0 hidden h-dvh w-56 shrink-0 flex-col gap-4 border-r border-border bg-card/40 p-3 lg:flex">
        <Brand />
        <NavItems activeCount={activeCount} />
        <div className="mt-auto flex items-center justify-between gap-2 px-1">
          <ResticStatus />
        </div>
      </aside>

      <div className="flex min-w-0 flex-1 flex-col">
        <header className="sticky top-0 z-30 flex h-14 items-center gap-2 border-b border-border bg-background/85 px-5 backdrop-blur sm:px-8">
          <MobileNav activeCount={activeCount} />
          <div className="lg:hidden">
            <Brand />
          </div>
          <div className="ml-auto flex items-center gap-1.5">
            <ActivityIndicator />
            <span className="hidden lg:inline">
              <ThemeToggle />
            </span>
            <span className="lg:hidden">
              <ResticStatus compact />
            </span>
            <span className="lg:hidden">
              <ThemeToggle />
            </span>
          </div>
        </header>

        <ResticBanner />

        <main className="flex-1">
          <Outlet />
        </main>

        <footer className="border-t border-border px-5 py-4 text-xs text-muted-foreground sm:px-8">
          Single-user local tool · no authentication · the backup engine is{" "}
          <code className="font-mono">restic</code>
        </footer>
      </div>
    </div>
  );
}
