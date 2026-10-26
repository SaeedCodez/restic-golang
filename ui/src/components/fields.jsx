import { CircleAlert, CircleCheck, Info, TriangleAlert } from "lucide-react";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { TONE_TEXT } from "@/components/kind";
import { cn } from "@/lib/utils";

/** Field is one labelled form control with an optional hint underneath. */
export function Field({ label, htmlFor, hint, className, children }) {
  return (
    <div className={cn("space-y-2", className)}>
      {label ? <Label htmlFor={htmlFor}>{label}</Label> : null}
      {children}
      {hint ? <p className="text-sm text-muted-foreground">{hint}</p> : null}
    </div>
  );
}

/**
 * CheckOption is a checkbox drawn as a card: what it does, one line on what
 * that means, and the box. The whole card is the click target, and it takes
 * the brand tint while ticked so the choice reads at a glance.
 */
export function CheckOption({ checked, onChange, disabled, title, description, className }) {
  return (
    <label
      className={cn(
        "flex cursor-pointer items-start gap-3 rounded-xl border border-border px-4 py-3.5 transition-colors",
        "hover:bg-accent/50 has-[:checked]:border-primary/40 has-[:checked]:bg-primary/5",
        "has-[:disabled]:cursor-not-allowed has-[:disabled]:opacity-60",
        className,
      )}
    >
      <input
        type="checkbox"
        className="mt-0.5 size-4 shrink-0 accent-primary"
        checked={checked}
        onChange={(e) => onChange(e.target.checked)}
        disabled={disabled}
      />
      <span className="min-w-0">
        <span className="block font-medium">{title}</span>
        {description ? (
          <span className="mt-0.5 block text-sm text-muted-foreground">{description}</span>
        ) : null}
      </span>
    </label>
  );
}

/**
 * SwitchRow turns a whole feature on or off (a schedule, a retention policy).
 * The title says what it is; the description says what it is set to now.
 */
export function SwitchRow({ id, checked, onCheckedChange, disabled, title, description, className }) {
  return (
    <div
      className={cn(
        "flex items-center justify-between gap-4 rounded-xl border border-border px-4 py-3.5 transition-colors",
        checked && "border-primary/30 bg-primary/5",
        className,
      )}
    >
      <label htmlFor={id} className="min-w-0 flex-1 cursor-pointer">
        <span className="block font-medium">{title}</span>
        {description ? (
          <span className="mt-0.5 block text-sm text-muted-foreground">{description}</span>
        ) : null}
      </label>
      <Switch id={id} checked={checked} onCheckedChange={onCheckedChange} disabled={disabled} />
    </div>
  );
}

/**
 * Segmented picks one of a few modes. Unlike FilterTabs, which narrows a list,
 * it changes what a form will do, so it is a radio group.
 *
 * options: [{ value, label, icon? }]
 */
export function Segmented({ options, value, onChange, tone, label, className }) {
  return (
    <div
      role="radiogroup"
      aria-label={label}
      className={cn("flex gap-1 rounded-xl bg-secondary p-1 text-sm", className)}
    >
      {options.map(({ value: v, label: text, icon: Icon }) => {
        const selected = v === value;
        return (
          <button
            key={v}
            type="button"
            role="radio"
            aria-checked={selected}
            onClick={() => onChange(v)}
            className={cn(
              "flex h-9 min-w-0 flex-1 items-center justify-center gap-2 rounded-lg px-3 font-medium transition-colors",
              selected
                ? "bg-card text-foreground shadow-[0_1px_2px_rgb(0_0_0/0.08)]"
                : "text-muted-foreground hover:text-foreground",
            )}
          >
            {Icon ? (
              <Icon className={cn("size-4 shrink-0", selected && tone ? TONE_TEXT[tone] : null)} />
            ) : null}
            <span className="truncate">{text}</span>
          </button>
        );
      })}
    </div>
  );
}

const CALLOUT = {
  info: { cls: "bg-primary/8 text-primary", icon: Info },
  success: { cls: "bg-success/10 text-success", icon: CircleCheck },
  warning: { cls: "bg-warning/12 text-warning", icon: TriangleAlert },
  destructive: { cls: "bg-destructive/8 text-destructive", icon: CircleAlert },
};

/**
 * Callout is a tinted note inside a card or dialog: a warning before a
 * destructive choice, the result of a check, a missing prerequisite.
 */
export function Callout({ tone = "info", icon, title, children, className }) {
  const { cls, icon: DefaultIcon } = CALLOUT[tone] || CALLOUT.info;
  const Icon = icon || DefaultIcon;
  return (
    <div
      role={tone === "destructive" || tone === "warning" ? "alert" : "status"}
      className={cn("flex items-start gap-3 rounded-xl px-4 py-3 text-sm", cls, className)}
    >
      <Icon className="mt-0.5 size-4 shrink-0" />
      <div className="min-w-0 flex-1 break-words">
        {title ? <p className="font-medium">{title}</p> : null}
        {children ? <div className={cn(title && "mt-0.5 opacity-90")}>{children}</div> : null}
      </div>
    </div>
  );
}

/** PathValue shows a fixed path (where a restore will write) as read-only text. */
export function PathValue({ children, className }) {
  return (
    <p
      className={cn(
        "rounded-xl border border-border bg-secondary/50 px-3.5 py-2.5 font-mono text-[0.8125rem] break-all",
        className,
      )}
    >
      {children}
    </p>
  );
}
