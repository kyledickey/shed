import { useEffect, useState } from "react";

export type Theme = "system" | "light" | "dark";

// index.html applies the stored theme before first paint using the same key.
const storageKey = "shed.theme";
const darkQuery = "(prefers-color-scheme: dark)";

function storedTheme(): Theme {
  const value = localStorage.getItem(storageKey);
  return value === "light" || value === "dark" ? value : "system";
}

/** applyMode sets data-mode, which Kumo and shed's tokens both key dark mode on. */
function applyMode(theme: Theme) {
  const dark = theme === "dark" || (theme === "system" && matchMedia(darkQuery).matches);
  document.documentElement.dataset.mode = dark ? "dark" : "light";
}

/** Returns the user's theme preference and a setter that applies and persists it. */
export function useTheme(): [Theme, (theme: Theme) => void] {
  const [theme, setTheme] = useState(storedTheme);
  useEffect(() => {
    if (theme !== "system") return;
    const media = matchMedia(darkQuery);
    const onChange = () => applyMode("system");
    media.addEventListener("change", onChange);
    return () => media.removeEventListener("change", onChange);
  }, [theme]);
  const apply = (next: Theme) => {
    if (next === "system") localStorage.removeItem(storageKey);
    else localStorage.setItem(storageKey, next);
    applyMode(next);
    setTheme(next);
  };
  return [theme, apply];
}

/** useDarkMode reports whether the dashboard is currently drawn dark. */
export function useDarkMode(): boolean {
  const read = () => document.documentElement.dataset.mode === "dark";
  const [dark, setDark] = useState(read);
  useEffect(() => {
    const observer = new MutationObserver(() => setDark(read()));
    observer.observe(document.documentElement, { attributeFilter: ["data-mode"] });
    return () => observer.disconnect();
  }, []);
  return dark;
}
