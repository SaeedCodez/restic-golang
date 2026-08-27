/** Default page size for list views — high enough for typical production use. */
export const PAGE_SIZE = 100;

/**
 * pageSlice returns the items for a 1-based page from an in-memory list.
 */
export function pageSlice(items, page, pageSize = PAGE_SIZE) {
  const list = items || [];
  const size = Math.max(1, pageSize);
  const totalPages = Math.max(1, Math.ceil(list.length / size));
  const safePage = Math.min(Math.max(1, page), totalPages);
  const start = (safePage - 1) * size;
  return {
    items: list.slice(start, start + size),
    page: safePage,
    totalPages,
    total: list.length,
    pageSize: size,
  };
}
