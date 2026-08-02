import { Toaster as Sonner } from "sonner";
import { useTheme } from "@/lib/theme";

/**
 * Toaster renders transient notifications. It replaces the alert() calls the
 * previous UI used, so a failure never blocks the page and always says what
 * happened in the app's own visual language.
 *
 * Sonner styles itself from CSS variables rather than classes, so the theme is
 * handed over as variables — that way its own stylesheet cannot win over ours.
 */
export function Toaster(props) {
  const { resolved } = useTheme();
  return (
    <Sonner
      theme={resolved}
      position="bottom-right"
      closeButton
      className="toaster group"
      style={{
        "--normal-bg": "var(--popover)",
        "--normal-text": "var(--popover-foreground)",
        "--normal-border": "var(--border)",
        "--success-bg": "var(--popover)",
        "--success-text": "var(--success)",
        "--success-border": "var(--border)",
        "--error-bg": "var(--popover)",
        "--error-text": "var(--destructive)",
        "--error-border": "var(--border)",
        "--warning-bg": "var(--popover)",
        "--warning-text": "var(--warning)",
        "--warning-border": "var(--border)",
        "--border-radius": "var(--radius)",
      }}
      toastOptions={{
        classNames: {
          title: "text-sm font-medium",
          description: "!text-muted-foreground text-[13px] leading-relaxed",
          actionButton:
            "!bg-primary !text-primary-foreground !rounded-md !text-xs !font-medium",
          cancelButton:
            "!bg-secondary !text-secondary-foreground !rounded-md !text-xs !font-medium",
        },
      }}
      {...props}
    />
  );
}
