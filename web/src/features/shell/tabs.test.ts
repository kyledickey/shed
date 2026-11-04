import { describe, expect, it } from "vite-plus/test";
import { parseTabs, tabsReducer, type Tab, type TabsAction, type TabsState } from "./tabs";

const tab = (id: string, href: string, pinned = false): Tab => ({ id, href, pinned });

const p = tab("p", "/settings", true);
const a = tab("a", "/");
const b = tab("b", "/server");
const three: TabsState = { tabs: [p, a, b], active: "a" };

describe("tabsReducer", () => {
  const cases: { name: string; state: TabsState; action: TabsAction; want: TabsState }[] = [
    {
      name: "visit moves the active tab",
      state: three,
      action: { type: "visit", href: "/projects/1" },
      want: { ...three, tabs: [p, tab("a", "/projects/1"), b] },
    },
    {
      name: "visit to another tab's place switches to it",
      state: three,
      action: { type: "visit", href: "/server" },
      want: { ...three, active: "b" },
    },
    {
      name: "arrive at a new place opens a tab beside the active one",
      state: three,
      action: { type: "arrive", href: "/docs", id: "n" },
      want: { tabs: [p, a, tab("n", "/docs"), b], active: "n" },
    },
    {
      name: "arrive at an open place reuses its tab",
      state: three,
      action: { type: "arrive", href: "/settings", id: "n" },
      want: { ...three, active: "p" },
    },
    {
      name: "open in the background keeps focus",
      state: three,
      action: { type: "open", href: "/docs", id: "n", focus: false },
      want: { tabs: [p, a, tab("n", "/docs"), b], active: "a" },
    },
    {
      name: "closing the active tab focuses its right neighbour",
      state: three,
      action: { type: "close", id: "a", newId: "n" },
      want: { tabs: [p, b], active: "b" },
    },
    {
      name: "closing the last tab on the right focuses its left neighbour",
      state: { ...three, active: "b" },
      action: { type: "close", id: "b", newId: "n" },
      want: { tabs: [p, a], active: "a" },
    },
    {
      name: "closing the only tab leaves a fresh home tab",
      state: { tabs: [tab("a", "/server")], active: "a" },
      action: { type: "close", id: "a", newId: "n" },
      want: { tabs: [tab("n", "/")], active: "n" },
    },
    {
      name: "close others keeps pinned tabs",
      state: three,
      action: { type: "closeOthers", id: "b" },
      want: { tabs: [p, b], active: "b" },
    },
    {
      name: "pinning moves a tab to the end of the pinned group",
      state: three,
      action: { type: "pin", id: "b", pinned: true },
      want: { ...three, tabs: [p, tab("b", "/server", true), a] },
    },
    {
      name: "unpinning moves a tab to the start of the unpinned group",
      state: three,
      action: { type: "pin", id: "p", pinned: false },
      want: { ...three, tabs: [tab("p", "/settings"), a, b] },
    },
    {
      name: "move cannot carry an unpinned tab into the pinned group",
      state: three,
      action: { type: "move", id: "b", to: 0 },
      want: { ...three, tabs: [p, b, a] },
    },
  ];
  for (const c of cases) {
    it(c.name, () => expect(tabsReducer(c.state, c.action)).toEqual(c.want));
  }
});

describe("parseTabs", () => {
  it("round-trips a valid state", () => {
    expect(parseTabs(JSON.stringify(three))).toEqual(three);
  });
  it("drops malformed tabs and repairs a missing active id", () => {
    const raw = JSON.stringify({
      tabs: [{ id: "x" }, tab("a", "https://evil.example"), tab("b", "/server")],
      active: "gone",
    });
    expect(parseTabs(raw)).toEqual({ tabs: [tab("b", "/server")], active: "b" });
  });
  it("rejects junk", () => {
    for (const raw of [null, "", "nope", "[]", '{"tabs":[],"active":"a"}']) {
      expect(parseTabs(raw)).toBeNull();
    }
  });
});
