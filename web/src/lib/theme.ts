import { useState } from "react";

export type Theme = "system" | "light" | "dark";

// index.html applies the stored theme before first paint using the same key.
const storageKey = "shed.theme";

function storedTheme(): Theme {
  const value = localStorage.getItem(storageKey);
  return value === "light" || value === "dark" ? value : "system";
}

/** Returns the user's theme preference and a setter that applies and persists it. */
export function useTheme(): [Theme, (theme: Theme) => void] {
  const [theme, setTheme] = useState(storedTheme);
  const apply = (next: Theme) => {
    if (next === "system") {
      localStorage.removeItem(storageKey);
      delete document.documentElement.dataset.theme;
    } else {
      localStorage.setItem(storageKey, next);
      document.documentElement.dataset.theme = next;
    }
    setTheme(next);
  };
  return [theme, apply];
}
