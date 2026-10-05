import { useLayoutEffect, useRef, useState, type ReactNode } from "react";

const MIN_HEIGHT = 320;
// Space left below the viewer: the page's bottom padding plus the shell frame.
const BOTTOM_GAP = 56;

/**
 * FillHeight gives its log viewer a list height that reaches the bottom of
 * the window, net of the viewer's toolbar.
 */
export function FillHeight({ children }: { children: (height: number) => ReactNode }) {
  const ref = useRef<HTMLDivElement>(null);
  const [height, setHeight] = useState(440);

  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const measure = () => {
      const list = el.querySelector<HTMLElement>('[role="log"]')?.parentElement;
      const chrome = el.offsetHeight - (list?.offsetHeight ?? 0);
      const available = window.innerHeight - el.getBoundingClientRect().top - BOTTOM_GAP;
      setHeight(Math.max(MIN_HEIGHT, Math.round(available - chrome)));
    };
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    window.addEventListener("resize", measure);
    return () => {
      ro.disconnect();
      window.removeEventListener("resize", measure);
    };
  }, []);

  return <div ref={ref}>{children(height)}</div>;
}
