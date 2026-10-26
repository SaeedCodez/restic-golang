import { TONE_TEXT } from "@/components/kind";
import { cn } from "@/lib/utils";

/**
 * FilterTabs is a segmented control that narrows a list in place. Each option
 * may carry an icon (tinted with its kind's hue when selected) and a count, so
 * you can see how many items sit behind a tab before choosing it.
 *
 * options: [{ value, label, icon?, tone?, count? }]
 */
export function FilterTabs({ options, value, onChange, label, className }) {
  return (
    <div
      role="tablist"
      aria-label={label}
      className={cn(
        "flex w-full gap-1 overflow-x-auto scroll-thin rounded-xl bg-secondary p-1 sm:w-max",
        className,
      )}
    >
      {options.map(({ value: v, label: text, icon: Icon, tone, count }) => {
        const selected = v === value;
        return (
          <button
            key={v}
            type="button"
            role="tab"
            aria-selected={selected}
            onClick={() => onChange(v)}
            className={cn(
              "flex h-9 flex-1 items-center justify-center gap-1.5 whitespace-nowrap rounded-lg px-2.5 text-sm font-medium transition-colors sm:flex-none sm:gap-2 sm:px-3.5",
              selected
                ? "bg-card text-foreground shadow-[0_1px_2px_rgb(0_0_0/0.08)]"
                : "text-muted-foreground hover:text-foreground",
            )}
          >
            {Icon ? (
              // Icons give way on phones so every tab fits without scrolling.
              <Icon
                className={cn(
                  "hidden size-4 shrink-0 sm:block",
                  selected && tone ? TONE_TEXT[tone] : null,
                )}
              />
            ) : null}
            {text}
            {count !== undefined ? (
              <span
                className={cn(
                  "rounded-full px-1.5 text-xs tabular",
                  selected ? "bg-secondary text-foreground" : "text-muted-foreground",
                )}
              >
                {count}
              </span>
            ) : null}
          </button>
        );
      })}
    </div>
  );
}
