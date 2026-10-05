import { useEffect, useState } from "react";

export type Accent = "grass" | "sky" | "grape" | "teal" | "tangerine" | "bubblegum";
export type Tweaks = { accent: Accent; soft: number };

const key = "shed.showcase.tweaks.v2";
const defaults: Tweaks = { accent: "grass", soft: 1 };

function load(): Tweaks {
  try {
    return { ...defaults, ...(JSON.parse(localStorage.getItem(key) ?? "{}") as Partial<Tweaks>) };
  } catch {
    return defaults;
  }
}

/** useTweaks holds the showcase's design knobs and applies them to <html>. */
export function useTweaks(): [Tweaks, (patch: Partial<Tweaks>) => void] {
  const [tweaks, setTweaks] = useState(load);
  useEffect(() => {
    const root = document.documentElement;
    root.dataset.accent = tweaks.accent;
    root.style.setProperty("--soft", String(tweaks.soft));
    localStorage.setItem(key, JSON.stringify(tweaks));
  }, [tweaks]);
  return [tweaks, (patch) => setTweaks((t) => ({ ...t, ...patch }))];
}
