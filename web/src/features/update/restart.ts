import { onlineManager, useQueryClient } from "@tanstack/react-query";
import { useCallback, useSyncExternalStore } from "react";

/** Restart is an install in flight: the version being installed and when waiting began. */
export type Restart = { version: string; startedAt: number };

let current: Restart | null = null;
const listeners = new Set<() => void>();

function set(next: Restart | null) {
  current = next;
  for (const l of listeners) l();
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

/** useRestart returns the restart in progress, or null. */
export function useRestart(): Restart | null {
  return useSyncExternalStore(subscribe, () => current);
}

/** restartAgain resets the wait clock of the restart in progress. */
export function restartAgain(now = Date.now()) {
  if (current) set({ ...current, startedAt: now });
}

/**
 * useBeginRestart returns a function that shows the restarting screen for an
 * install of `version`. The server is about to go away, so it also stops
 * query traffic: nothing refetches while the screen is up, and the page
 * reload that ends it starts fresh.
 */
export function useBeginRestart() {
  const qc = useQueryClient();
  return useCallback(
    (version: string) => {
      void qc.cancelQueries();
      onlineManager.setOnline(false);
      set({ version, startedAt: Date.now() });
    },
    [qc],
  );
}

/** hasRestarted reports whether an /api/update body shows `version` running. */
export function hasRestarted(body: unknown, version: string): boolean {
  return (
    typeof body === "object" && body !== null && (body as { current?: unknown }).current === version
  );
}

/** timeoutMs is how long the restarting screen waits before offering recovery hints. */
export const timeoutMs = 3 * 60_000;
