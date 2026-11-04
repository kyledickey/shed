import { useState } from "react";

/** PAGE_SIZE is how many deployments or backups a list shows at a time. */
export const PAGE_SIZE = 10;

/** pagePath adds the limit and before parameters of a paged list to path. */
export function pagePath(path: string, before: string | null, limit = PAGE_SIZE): string {
  const q = new URLSearchParams({ limit: String(limit) });
  if (before !== null) q.set("before", before);
  return `${path}?${q}`;
}

/** CursorPages is the position in a list paged by cursor, with the cursors needed to go back. */
export type CursorPages = {
  /** The before of the page on screen; null on the first. */
  before: string | null;
  hasPrevious: boolean;
  /** next moves to the page after the one whose next cursor is given. */
  next: (cursor: string) => void;
  previous: () => void;
};

/**
 * useCursorPages tracks which page of a cursor-paged list is on screen. The
 * API only pages forward, so it keeps the cursor of every page passed.
 */
export function useCursorPages(): CursorPages {
  const [stack, setStack] = useState<(string | null)[]>([null]);
  return {
    before: stack[stack.length - 1] ?? null,
    hasPrevious: stack.length > 1,
    next: (cursor) => setStack((s) => [...s, cursor]),
    previous: () => setStack((s) => (s.length > 1 ? s.slice(0, -1) : s)),
  };
}
