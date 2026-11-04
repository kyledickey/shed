# Dashboard design

shed's dashboard follows the look of Cloudflare's dashboard, built from Kumo
(`@cloudflare/kumo`) components and tokens, with shed's green accent and
Open Runde. The values below were measured from Cloudflare's Workers pages
in light and dark. Prefer a Kumo component whenever one exists, and build
anything else from the patterns here with Kumo's CSS variables.

## Principles

- **Quiet surfaces, structure from rings.** Depth comes from a 1px ring and a
  slight change of fill, never from shadows, gradients, or color.
- **Color is for meaning only.** Neutral everywhere. Green marks the primary
  action. Status colors appear only as a thin status bar, the one spinner on
  a running step, or the text of an error.
- **No decoration.** No colored icon tiles, no tinted pill badges, no status
  dots, no illustrations. Icons are plain 16px glyphs in the text color.
- **Text does the work.** Labels on the left, values on the right, joined by
  a leader line or aligned in a table. Sentence case everywhere.

## Type

| Use | Size / weight | Color |
| --- | --- | --- |
| Page title | 20px / 600 | default |
| Section heading (h2) | 16px / 500 | default |
| Card title, setting label | 14px / 500 | card titles subtle, setting labels default |
| Body, table cells, values | 14px / 400 | default |
| Tabs, nav, node titles | 13px / 500 | default |
| Small labels (stat names, column heads) | 12px / 500 | subtle |
| Big numbers (stats) | 24px / 500, tabular | default |
| Pills | 12px / 500 | default |

Inline code and IDs use Commit Mono at about 0.9em.

## Surfaces

All values are Kumo tokens. In dark mode they resolve to: page `canvas`
L0.10, card header `elevated` L0.12, diagram canvas `recessed` L0.145,
panels `base` L0.17, inner rings `hairline` L0.27, outer rings `line` L0.32.
In light mode panels are white and rings are 10% black.

- **Card** (Kumo `LayerCard`): outer radius 8px, `elevated` header strip
  (16px padding, 14px/500 subtle title, optional trailing arrow link), inner
  `base` panel with its own hairline ring. Never nest cards.
- **Frame** (diagram nodes, grouped controls): 4px padding, radius 12px, a
  hairline border, around an inner panel of radius 8px. Radii are concentric.
- **Setting row**: a `base` panel, radius 12px, ring `line`, 16px padding.
  The label (14px/500) sits left and the control right. Extra options
  continue the row under a full-width hairline, on the same fill, laid out
  like the row itself. Numbers inside a sentence ("Above [90]% for [10] min")
  are compact inline fields: mono, as wide as the value, a hairline ring that
  fills a step on hover, units as plain text.
- **Help tooltip**: a label explained in the docs gets a dashed underline
  (offset 4px, ~35% opacity, full on hover) and a help cursor. Hover or focus
  shows the summary; click opens the docs. No `?` icons.
- **Empty well**: inside a card or row, radius 8px, 1px border (dashed in
  dark), `recessed` fill, 8px 16px padding, subtle 13px text, optional
  trailing small button.
- **Banner**: one line, radius 8px, `base` fill, ring `line`, 48px tall.

## Patterns

- **Page header**: a trail, then the title row with actions on the right,
  then section tabs as a segmented group (radius 8px track, 32px tabs with
  0 10px padding and radius 6px, the active tab raised with a ring).
- **Settings page**: sections stacked with 40px between them. Each section is
  an h2 with optional actions on the right, then setting rows 12px apart. A
  sticky table of contents on the right lists the sections, with a bar on the
  current one. Danger zone comes last, and its entry is red.
- **Lists of records** (deployments, backups, audit): rows 44px tall inside a
  card, cells padded 8px 16px. Monospace ID, italic subtle message, status
  bar and word, outline pill for the source, actor, relative time, and a
  trailing `⋯` menu.
- **Tables**: column heads 12px/500 subtle on a header row a step off `base`,
  8px 12px padding, hairline under the header. Empty tables show an empty
  well instead of rows.
- **Stats**: a strip of cells divided by hairlines, each with a small label,
  a big number, an optional change (green up, red down), and a sparkline
  along the bottom. "No data" is an outline pill over a flat line.
- **Charts**: Kumo `TimeseriesChart` in a card. The legend sits above the
  plot as a colored square, a name, and the current value. Axes are 12px subtle.
- **Pills**: 2px 8px padding, full radius, 1px hairline ring, `base` fill.
  Count chips are a small grey square with the number.
- **Status**: `StatusBadge`, a 3px colored bar and a word, in progress too.
  A view has at most one spinner, on the step that is running.
- **Buttons**: Kumo `Button`. Secondary for most actions, primary (green) for
  the one action per view, ghost for icon buttons, destructive only in the
  danger zone.
- **Create flows**: a regular modal dialog (`FormDialog`), never a
  full-screen takeover. Multi-step flows change the dialog's title and body in
  place, with Back beside the primary action.

## Checking a page

Run `bun run mock` and `bun run dev`, open the page against the mock API, and
look at it in light and dark at 1440px and 390px wide before calling it done.
