/**
 * Tabs are shed's own browser-style tabs: each one remembers a location in
 * the dashboard, so several places stay one click apart. The router's URL is
 * always the active tab's location.
 */

export type Tab = { id: string; href: string; pinned: boolean };

/** TabsState always holds at least one tab, and `active` names one of them. */
export type TabsState = { tabs: Tab[]; active: string };

export type TabsAction =
  /** The page loaded at href: show the tab already there, or open one. */
  | { type: "arrive"; href: string; id: string }
  /** The router moved: the active tab follows, unless another tab is already there. */
  | { type: "visit"; href: string }
  | { type: "open"; href: string; id: string; focus: boolean }
  | { type: "activate"; id: string }
  | { type: "close"; id: string; newId: string }
  | { type: "closeOthers"; id: string }
  | { type: "pin"; id: string; pinned: boolean }
  | { type: "move"; id: string; to: number };

export const homeHref = "/";

export function initialTabs(id: string, href = homeHref): TabsState {
  return { tabs: [{ id, href, pinned: false }], active: id };
}

export function activeTab(state: TabsState): Tab {
  const tab = state.tabs.find((t) => t.id === state.active) ?? state.tabs[0];
  if (!tab) throw new Error("tabs: a TabsState must hold at least one tab");
  return tab;
}

/** Pinned tabs always lead the strip, in the order they were pinned. */
function ordered(tabs: Tab[]): Tab[] {
  return [...tabs.filter((t) => t.pinned), ...tabs.filter((t) => !t.pinned)];
}

function insertAfterActive(state: TabsState, tab: Tab): Tab[] {
  const at = state.tabs.findIndex((t) => t.id === state.active);
  const tabs = [...state.tabs];
  tabs.splice(at + 1, 0, tab);
  return ordered(tabs);
}

export function tabsReducer(state: TabsState, action: TabsAction): TabsState {
  switch (action.type) {
    case "arrive": {
      if (activeTab(state).href === action.href) return state;
      const existing = state.tabs.find((t) => t.href === action.href);
      if (existing) return { ...state, active: existing.id };
      return {
        tabs: insertAfterActive(state, { id: action.id, href: action.href, pinned: false }),
        active: action.id,
      };
    }
    case "visit": {
      const current = activeTab(state);
      if (current.href === action.href) return state;
      const existing = state.tabs.find((t) => t.href === action.href);
      if (existing) return { ...state, active: existing.id };
      return {
        ...state,
        tabs: state.tabs.map((t) => (t.id === current.id ? { ...t, href: action.href } : t)),
      };
    }
    case "open": {
      const tabs = insertAfterActive(state, { id: action.id, href: action.href, pinned: false });
      return { tabs, active: action.focus ? action.id : state.active };
    }
    case "activate":
      return state.tabs.some((t) => t.id === action.id) ? { ...state, active: action.id } : state;
    case "close": {
      const at = state.tabs.findIndex((t) => t.id === action.id);
      if (at < 0) return state;
      const tabs = state.tabs.filter((t) => t.id !== action.id);
      const next = tabs[Math.min(at, tabs.length - 1)];
      if (!next) return initialTabs(action.newId);
      if (state.active !== action.id) return { ...state, tabs };
      return { tabs, active: next.id };
    }
    case "closeOthers": {
      const tabs = state.tabs.filter((t) => t.id === action.id || t.pinned);
      return { tabs, active: action.id };
    }
    case "pin": {
      const tabs = state.tabs.filter((t) => t.id !== action.id);
      const tab = state.tabs.find((t) => t.id === action.id);
      if (!tab) return state;
      const pinnedCount = tabs.filter((t) => t.pinned).length;
      tabs.splice(pinnedCount, 0, { ...tab, pinned: action.pinned });
      return { ...state, tabs };
    }
    case "move": {
      const tab = state.tabs.find((t) => t.id === action.id);
      if (!tab) return state;
      const tabs = state.tabs.filter((t) => t.id !== action.id);
      // A tab stays inside its own group: pinned tabs never mix with the rest.
      const pinnedCount = tabs.filter((t) => t.pinned).length;
      const [min, max] = tab.pinned ? [0, pinnedCount] : [pinnedCount, tabs.length];
      tabs.splice(Math.min(Math.max(action.to, min), max), 0, tab);
      return { ...state, tabs };
    }
  }
}

/** parseTabs reads stored tabs, or returns null when the value isn't a valid TabsState. */
export function parseTabs(raw: string | null): TabsState | null {
  if (!raw) return null;
  try {
    const value: unknown = JSON.parse(raw);
    if (typeof value !== "object" || value === null) return null;
    const { tabs, active } = value as Record<string, unknown>;
    if (!Array.isArray(tabs) || typeof active !== "string") return null;
    const valid = tabs.filter(
      (t): t is Tab =>
        typeof t === "object" &&
        t !== null &&
        typeof t.id === "string" &&
        typeof t.href === "string" &&
        t.href.startsWith("/") &&
        typeof t.pinned === "boolean",
    );
    const first = valid[0];
    if (!first) return null;
    const ids = new Set(valid.map((t) => t.id));
    return { tabs: ordered(valid), active: ids.has(active) ? active : first.id };
  } catch {
    return null;
  }
}
