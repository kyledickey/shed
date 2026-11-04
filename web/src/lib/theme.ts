import { useEffect, useState, useSyncExternalStore } from "react";

export type Theme = "system" | "light" | "dark";

// index.html applies the stored theme before first paint using the same key.
const storageKey = "shed.theme";
const darkQuery = "(prefers-color-scheme: dark)";

function storedTheme(): Theme {
  let value: string | null = null;
  try {
    value = localStorage.getItem(storageKey);
  } catch {}
  return value === "light" || value === "dark" ? value : "system";
}

/** applyMode sets data-mode, which Kumo and shed's tokens both key dark mode on. */
function applyMode(theme: Theme) {
  const dark = theme === "dark" || (theme === "system" && matchMedia(darkQuery).matches);
  document.documentElement.dataset.mode = dark ? "dark" : "light";
}

// Every useTheme shares the stored preference, so a choice made in one control
// shows in the others and stops system mode from following the OS.
const listeners = new Set<() => void>();

function subscribe(listener: () => void) {
  listeners.add(listener);
  const media = matchMedia(darkQuery);
  const onChange = () => {
    if (storedTheme() === "system") applyMode("system");
  };
  media.addEventListener("change", onChange);
  return () => {
    listeners.delete(listener);
    media.removeEventListener("change", onChange);
  };
}

function setStoredTheme(next: Theme) {
  try {
    if (next === "system") localStorage.removeItem(storageKey);
    else localStorage.setItem(storageKey, next);
  } catch {}
  applyMode(next);
  for (const listener of listeners) listener();
}

/** Returns the user's theme preference and a setter that applies and persists it. */
export function useTheme(): [Theme, (theme: Theme) => void] {
  // Prerendered pages start at "system"; the stored choice is read after hydration.
  const theme = useSyncExternalStore(subscribe, storedTheme, () => "system" as const);
  return [theme, setStoredTheme];
}

/** useDarkMode reports whether the dashboard is currently drawn dark. */
export function useDarkMode(): boolean {
  // Prerendering has no document; the effect corrects it after hydration.
  const read = () =>
    typeof document !== "undefined" && document.documentElement.dataset.mode === "dark";
  const [dark, setDark] = useState(read);
  useEffect(() => {
    setDark(read());
    const observer = new MutationObserver(() => setDark(read()));
    observer.observe(document.documentElement, { attributeFilter: ["data-mode"] });
    return () => observer.disconnect();
  }, []);
  return dark;
}
