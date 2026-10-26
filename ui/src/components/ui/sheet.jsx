import * as React from "react";
import * as DialogPrimitive from "@radix-ui/react-dialog";
import { X } from "lucide-react";
import { cn } from "@/lib/utils";

/** Sheet is a dialog that slides in from the left edge — the mobile navigation drawer. */
const Sheet = DialogPrimitive.Root;
const SheetTrigger = DialogPrimitive.Trigger;
const SheetClose = DialogPrimitive.Close;
const SheetTitle = DialogPrimitive.Title;

const SheetContent = React.forwardRef(({ className, children, ...props }, ref) => (
  <DialogPrimitive.Portal>
    <DialogPrimitive.Overlay
      className={cn(
        "fixed inset-0 z-50 bg-black/45 backdrop-blur-[3px] dark:bg-black/65",
        "data-[state=open]:animate-in data-[state=closed]:animate-out",
        "data-[state=open]:fade-in-0 data-[state=closed]:fade-out-0",
      )}
    />
    <DialogPrimitive.Content
      ref={ref}
      aria-describedby={undefined}
      className={cn(
        "fixed inset-y-0 left-0 z-50 flex w-[min(20rem,calc(100vw-3rem))] flex-col border-r border-border bg-sidebar shadow-2xl",
        "data-[state=open]:animate-in data-[state=closed]:animate-out",
        "data-[state=open]:slide-in-from-left data-[state=closed]:slide-out-to-left",
        "duration-200",
        className,
      )}
      {...props}
    >
      {children}
      <DialogPrimitive.Close
        className="absolute right-3 top-4 rounded-lg p-1.5 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
        aria-label="Close menu"
      >
        <X className="size-[18px]" />
      </DialogPrimitive.Close>
    </DialogPrimitive.Content>
  </DialogPrimitive.Portal>
));
SheetContent.displayName = "SheetContent";

export { Sheet, SheetTrigger, SheetClose, SheetContent, SheetTitle };
