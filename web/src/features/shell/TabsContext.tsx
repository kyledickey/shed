import { useLocation, useRouter } from "@tanstack/react-router";
import {
  createContext,
  useContext,
  useEffect,
  useReducer,
  useRef,
  type Dispatch,
  type ReactNode,
} from "react";
import {
  activeTab,
  initialTabs,
  parseTabs,
  tabsReducer,
  type TabsAction,
  type TabsState,
} from "./tabs";

const storageKey = "shed.tabs";

type TabsContextValue = {
  state: TabsState;
  dispatch: Dispatch<TabsAction>;
  /** open puts href in a new tab, focused or in the background. */
  open: (href: string, focus?: boolean) => void;
  close: (id: string) => void;
};

const TabsContext = createContext<TabsContextValue | null>(null);

export function useTabs(): TabsContextValue {
  const value = useContext(TabsContext);
  if (!value) throw new Error("useTabs outside TabsProvider");
  return value;
}

const newId = () => crypto.randomUUID();

function load(href: string): TabsState {
  let stored: TabsState | null = null;
  try {
    stored = parseTabs(localStorage.getItem(storageKey));
  } catch {
    // Storage can be unavailable (private windows); tabs then last one visit.
  }
  return tabsReducer(stored ?? initialTabs(newId(), href), { type: "arrive", href, id: newId() });
}

/**
 * TabsProvider keeps the tab strip and the router in step: the URL is always
 * the active tab's place, and switching tabs navigates to the tab's place.
 */
export function TabsProvider({ children }: { children: ReactNode }) {
  const router = useRouter();
  const href = useLocation({ select: (l) => l.href });
  const [state, dispatch] = useReducer(tabsReducer, href, load);

  useEffect(() => dispatch({ type: "visit", href }), [href]);

  const target = activeTab(state).href;
  const first = useRef(true);
  useEffect(() => {
    if (first.current) {
      first.current = false;
      return;
    }
    if (router.state.location.href !== target) void router.navigate({ href: target });
  }, [state.active, target, router]);

  useEffect(() => {
    try {
      localStorage.setItem(storageKey, JSON.stringify(state));
    } catch {
      // See load.
    }
  }, [state]);

  const value: TabsContextValue = {
    state,
    dispatch,
    open: (to, focus = true) => dispatch({ type: "open", href: to, id: newId(), focus }),
    close: (id) => dispatch({ type: "close", id, newId: newId() }),
  };

  useOpenInTab(value.open);

  return <TabsContext.Provider value={value}>{children}</TabsContext.Provider>;
}

/**
 * useOpenInTab turns ⌘/Ctrl-click and middle-click on dashboard links into
 * shed tabs instead of browser tabs. Shift focuses the new tab.
 */
function useOpenInTab(open: (href: string, focus?: boolean) => void) {
  const openRef = useRef(open);
  openRef.current = open;
  useEffect(() => {
    const onClick = (e: MouseEvent) => {
      const wantsTab = e.button === 1 || (e.button === 0 && (e.metaKey || e.ctrlKey));
      if (!wantsTab || e.defaultPrevented) return;
      const anchor = (e.target as Element | null)?.closest?.("a[href]");
      if (!(anchor instanceof HTMLAnchorElement)) return;
      if (anchor.target === "_blank" || anchor.hasAttribute("download")) return;
      const url = new URL(anchor.href);
      if (url.origin !== location.origin || url.pathname.startsWith("/api/")) return;
      e.preventDefault();
      openRef.current(url.pathname + url.search + url.hash, e.shiftKey);
    };
    document.addEventListener("click", onClick, true);
    document.addEventListener("auxclick", onClick, true);
    return () => {
      document.removeEventListener("click", onClick, true);
      document.removeEventListener("auxclick", onClick, true);
    };
  }, []);
}
