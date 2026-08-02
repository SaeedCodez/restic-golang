import { Link } from "react-router-dom";
import { ChevronRight } from "lucide-react";
import { cn } from "@/lib/utils";

/** Page frames every screen: consistent width, spacing and heading rhythm. */
export function Page({ className, children }) {
  return (
    <div className={cn("mx-auto w-full max-w-5xl px-5 py-6 sm:px-8 sm:py-8", className)}>
      {children}
    </div>
  );
}

export function PageHeader({ title, description, actions, breadcrumb, className }) {
  return (
    <header className={cn("mb-6", className)}>
      {breadcrumb}
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h1 className="truncate text-xl font-semibold tracking-tight">{title}</h1>
          {description ? (
            <p className="mt-1 max-w-2xl text-[13px] leading-relaxed text-muted-foreground">
              {description}
            </p>
          ) : null}
        </div>
        {actions ? <div className="flex shrink-0 items-center gap-2">{actions}</div> : null}
      </div>
    </header>
  );
}

export function Breadcrumb({ to, label, current }) {
  return (
    <nav className="mb-2 flex items-center gap-1 text-[13px] text-muted-foreground">
      <Link to={to} className="rounded transition-colors hover:text-foreground">
        {label}
      </Link>
      <ChevronRight className="size-3.5 opacity-50" />
      <span className="truncate text-foreground">{current}</span>
    </nav>
  );
}

/** Mono renders paths, ids and other machine text in the monospace voice. */
export function Mono({ className, children, ...props }) {
  return (
    <span className={cn("font-mono text-xs break-all", className)} {...props}>
      {children}
    </span>
  );
}
