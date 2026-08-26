import { useTheme } from "@/lib/theme";
import { Button } from "@/components/ui/button";
import { Monitor, Moon, Sun } from "lucide-react";

/**
 * AuthLayout is the framed screen for setup and login: same brand mark as the
 * app shell, no navigation, just the form card.
 */
export function AuthLayout({ title, description, children }) {
  return (
    <div className="flex min-h-dvh flex-col bg-background">
      <header className="border-b border-border">
        <div className="mx-auto flex w-full max-w-md items-center justify-between px-5 py-3 sm:px-0">
          <div className="flex items-center gap-2.5">
            <span className="grid size-7 place-items-center rounded-md bg-primary text-primary-foreground">
              <svg viewBox="0 0 24 24" className="size-4" fill="none" aria-hidden="true">
                <path
                  d="M12 4a8 8 0 1 0 8 8"
                  stroke="currentColor"
                  strokeWidth="2.5"
                  strokeLinecap="round"
                />
                <path
                  d="M12 8v4l3 2"
                  stroke="currentColor"
                  strokeWidth="2.5"
                  strokeLinecap="round"
                />
              </svg>
            </span>
            <span className="display text-[13px] font-medium">restic</span>
          </div>
          <ThemeCycle />
        </div>
      </header>

      <main className="flex flex-1 items-start justify-center px-5 py-12 sm:py-16">
        <div className="w-full max-w-md">
          <h1 className="display text-[26px] font-semibold leading-tight">{title}</h1>
          {description ? (
            <p className="mt-2 text-sm leading-relaxed text-muted-foreground">{description}</p>
          ) : null}
          <div className="mt-8">{children}</div>
        </div>
      </main>
    </div>
  );
}

function ThemeCycle() {
  const { resolved, setTheme } = useTheme();
  const next = resolved === "dark" ? "light" : "dark";
  const Icon = resolved === "dark" ? Moon : Sun;
  return (
    <Button
      variant="ghost"
      size="icon-sm"
      aria-label={`Switch to ${next} theme`}
      onClick={() => setTheme(next)}
    >
      <Icon className="size-4" />
      <span className="sr-only">
        <Monitor />
      </span>
    </Button>
  );
}
