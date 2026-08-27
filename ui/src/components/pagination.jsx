import { ChevronLeft, ChevronRight } from "lucide-react";
import { Button } from "@/components/ui/button";

/**
 * ListPagination shows range text and previous/next controls when a list spans
 * more than one page. Pass total=0 (or omit) to hide.
 */
export function ListPagination({
  page,
  pageSize,
  total,
  onPageChange,
  className = "",
}) {
  if (!total || total <= pageSize) return null;

  const totalPages = Math.max(1, Math.ceil(total / pageSize));
  const safePage = Math.min(Math.max(1, page), totalPages);
  const from = (safePage - 1) * pageSize + 1;
  const to = Math.min(safePage * pageSize, total);

  return (
    <div
      className={`flex flex-wrap items-center justify-between gap-3 px-1 py-2 ${className}`}
    >
      <p className="text-[13px] text-muted-foreground">
        Showing{" "}
        <span className="tabular text-foreground">
          {from}–{to}
        </span>{" "}
        of <span className="tabular text-foreground">{total}</span>
      </p>
      <div className="flex items-center gap-2">
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={safePage <= 1}
          onClick={() => onPageChange(safePage - 1)}
          aria-label="Previous page"
        >
          <ChevronLeft />
          Previous
        </Button>
        <span className="tabular text-[13px] text-muted-foreground">
          {safePage} / {totalPages}
        </span>
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={safePage >= totalPages}
          onClick={() => onPageChange(safePage + 1)}
          aria-label="Next page"
        >
          Next
          <ChevronRight />
        </Button>
      </div>
    </div>
  );
}
