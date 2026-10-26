import { BrandMark, ThemeSwitch } from "@/components/brand";

/** The kind hues in a row: the brand's colour band, worn on the top edge of the card. */
const BAND = ["bg-primary", "bg-violet", "bg-cyan", "bg-fuchsia"];

/**
 * AuthLayout is the framed screen for setup and login: the brand, one card
 * holding the form, and the theme switch — no navigation.
 */
export function AuthLayout({ title, description, children, footnote }) {
  return (
    <div className="flex min-h-dvh flex-col bg-background">
      <header className="flex justify-end px-5 py-4 sm:px-8">
        <ThemeSwitch />
      </header>

      <main className="flex flex-1 items-start justify-center px-5 pb-16 pt-4 sm:items-center sm:pt-0">
        <div className="w-full max-w-[27rem]">
          <div className="mb-8 flex items-center gap-3.5">
            <BrandMark className="size-12 rounded-[14px]" />
            <div className="leading-tight">
              <p className="display text-lg font-semibold">restic</p>
              <p className="text-sm text-muted-foreground">backup manager</p>
            </div>
          </div>

          <div className="overflow-hidden rounded-2xl border border-border bg-card shadow-xl shadow-black/[0.04]">
            <div className="flex h-1" aria-hidden="true">
              {BAND.map((c) => (
                <span key={c} className={`flex-1 ${c}`} />
              ))}
            </div>
            <div className="p-6 sm:p-8">
              <h1 className="display text-2xl font-semibold leading-tight">{title}</h1>
              {description ? (
                <p className="mt-2 text-base text-muted-foreground">{description}</p>
              ) : null}
              <div className="mt-7">{children}</div>
            </div>
          </div>

          {footnote ? (
            <p className="mt-6 text-center text-sm text-muted-foreground">{footnote}</p>
          ) : null}
        </div>
      </main>
    </div>
  );
}
