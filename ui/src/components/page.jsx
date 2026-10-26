import { Link } from "react-router-dom";
import { ArrowRight, ChevronRight } from "lucide-react";
import { cn } from "@/lib/utils";

/** Page frames every screen: consistent width, spacing and heading rhythm. */
export function Page({ className, children }) {
  return (
    <div
      className={cn(
        "mx-auto w-full max-w-[1180px] px-5 pb-16 pt-8 sm:px-8 sm:pt-10 xl:px-12 xl:pt-12",
        className,
      )}
    >
      {children}
    </div>
  );
}

/**
 * PageHeader titles a screen. Detail screens pass `icon` (an IconTile in the
 * kind's hue) so a job, a repository or a run is recognisable before it is read.
 */
export function PageHeader({ title, description, actions, breadcrumb, icon, className }) {
  return (
    <header className={cn("mb-8 sm:mb-10", className)}>
      {breadcrumb}
      <div className="flex flex-wrap items-end justify-between gap-x-6 gap-y-4">
        <div className="flex min-w-0 max-w-3xl items-start gap-4">
          {icon ? <div className="hidden pt-0.5 sm:block">{icon}</div> : null}
          <div className="min-w-0">
            <h1 className="display break-words text-2xl font-semibold leading-tight sm:text-3xl">
              {title}
            </h1>
            {description ? (
              <div className="mt-2.5 text-base text-muted-foreground sm:text-md">{description}</div>
            ) : null}
          </div>
        </div>
        {actions ? <div className="flex flex-wrap items-center gap-2.5">{actions}</div> : null}
      </div>
    </header>
  );
}

export function Breadcrumb({ to, label, current }) {
  return (
    <nav className="mb-4 flex min-w-0 items-center gap-1.5 text-sm text-muted-foreground">
      <Link to={to} className="shrink-0 rounded transition-colors hover:text-foreground">
        {label}
      </Link>
      <ChevronRight className="size-4 shrink-0 opacity-50" />
      <span className="truncate text-foreground">{current}</span>
    </nav>
  );
}

/**
 * Section is a titled block within a page: heading on the left, an optional
 * "see all" link or control on the right.
 */
export function Section({ title, description, action, className, children }) {
  return (
    <section className={cn("space-y-4", className)}>
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div className="min-w-0">
          <h2 className="display text-lg font-semibold">{title}</h2>
          {description ? (
            <p className="mt-1 text-sm text-muted-foreground">{description}</p>
          ) : null}
        </div>
        {action}
      </div>
      {children}
    </section>
  );
}

/** SeeAll is the quiet "go to the full list" link used in section headers and card footers. */
export function SeeAll({ to, children }) {
  return (
    <Link
      to={to}
      className="group inline-flex items-center gap-1.5 text-sm font-medium text-muted-foreground transition-colors hover:text-primary"
    >
      {children}
      <ArrowRight className="size-4 transition-transform group-hover:translate-x-0.5" />
    </Link>
  );
}

/** Mono renders paths, ids and other machine text in the monospace voice. */
export function Mono({ className, children, ...props }) {
  return (
    <span className={cn("font-mono text-[0.8125rem] break-all", className)} {...props}>
      {children}
    </span>
  );
}

/**
 * Facts is a short definition list of a thing's settings or outcome, laid out
 * as a grid of label-over-value pairs. Items with no value are skipped.
 *
 * items: [{ label, value, mono?, wide? }]
 */
export function Facts({ items, className }) {
  const shown = items.filter((i) => i && i.value !== undefined && i.value !== null && i.value !== "");
  return (
    <dl className={cn("grid grid-cols-1 gap-x-8 gap-y-5 sm:grid-cols-2", className)}>
      {shown.map((item) => (
        <div key={item.label} className={cn("min-w-0", item.wide && "sm:col-span-2")}>
          <dt className="text-sm text-muted-foreground">{item.label}</dt>
          <dd
            className={cn(
              "mt-1 min-w-0 break-words",
              item.mono ? "font-mono text-[0.8125rem] break-all" : "font-medium",
            )}
          >
            {item.value}
          </dd>
        </div>
      ))}
    </dl>
  );
}
