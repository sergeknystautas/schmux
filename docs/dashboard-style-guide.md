# schmux Dashboard Style Guide

**Status:** Active — the design system for the schmux web dashboard. New and changed
UI is reviewed against this guide.

## Purpose and scope

This guide codifies the design system already practiced by the best surfaces of the
dashboard: the **left sidebar** (header, spawn button, workspace list, auth, tools),
the **main workbench** (session detail with xterm + secondary tabs, spawn wizard),
and the **Config** and **Environment** pages. Every other page is expected to
converge on these conventions.

It defines what a page should look like — structure, color, type, spacing, and the
standard interaction conventions. It is deliberately **not** an exhaustive component
library; a page that needs something this guide doesn't cover should build it from
the tokens and primitives here, not invent a parallel style.

**How this fits the other dashboard docs:** this is the _visual_ layer. For dashboard
UX principles, routes, and feature behavior see [`web.md`](web.md); for the internals
of specific UI subsystems (sidebar, action dropdowns, event monitor, spawn flow) see
[`dashboard-ui.md`](dashboard-ui.md).

**Reference surfaces, in order of authority:**

1. Sidebar + session detail + spawn (`AppShell.tsx`, `SessionDetailPage.tsx`, `SessionTabs.tsx`, `SpawnPage.tsx`, `WorkspaceHeader.tsx`)
2. Environment page (`EnvironmentPage.tsx`) — the cleanest "tool page" template
3. Config page (`ConfigPage.tsx` + `routes/config/`) — the tabbed-page template (internally inconsistent in places; follow its shell, not every tab)

---

## 1. Design tokens — the law

All tokens are defined at the top of `assets/dashboard/src/styles/global.css`
(`:root`, lines ~2–64). The theme is redefined three ways: `@media
(prefers-color-scheme: dark)`, `[data-theme='dark']`, and `[data-theme='light']`.

> **Rule zero: never hardcode a color, spacing, radius, shadow, font, or duration
> that a token already expresses.** A hardcoded hex value is not just inconsistent —
> it visibly breaks dark mode. This is the single most common compliance failure.

**Sanctioned hardcoded-color exceptions.** These are the only colors that may stay
literal — no token applies and they are intentionally theme-independent. Anything else
hardcoded is non-compliant.

- Terminal-emulator themes (xterm / asciinema — e.g. `CastPlayer`,
  `ConnectionProgressModal` terminal background).
- Third-party brand colors (e.g. the VS Code logo `#007ACC`).
- User-data color values, not UI chrome (e.g. a persona's chosen color, default
  `#3498db`).
- Two decorative one-off accents with no semantic token: backburner-status lavender
  `#b8a9e0` (`WorkspaceHeader`) and rating/vote gold `#f5c518` (`HomePage`).

New exceptions get appended here with the file and a one-line reason — the list is not
a license to skip tokenization.

### Color

| Token                                   | Light                | Role                                               |
| --------------------------------------- | -------------------- | -------------------------------------------------- |
| `--color-surface`                       | `#ffffff`            | Panels, cards, content areas                       |
| `--color-surface-alt`                   | `#f5f5f5`            | App/body background, card headers, secondary fills |
| `--color-surface-elevated`              | `#ffffff`            | Popovers, dropdowns                                |
| `--color-text`                          | `#1a1a1a`            | Primary text                                       |
| `--color-text-muted`                    | `#666666`            | Secondary text, labels of lesser rank              |
| `--color-text-faint`                    | `#888888`            | Hints, timestamps, tertiary text                   |
| `--color-border`                        | `#ddd`               | Standard 1px borders                               |
| `--color-border-subtle`                 | `#eee`               | Hairlines, internal dividers                       |
| `--color-accent`                        | `#0066cc`            | Primary actions, active states, links              |
| `--color-accent-hover`                  | `#0052a3`            | Accent hover                                       |
| `--color-accent-subtle`                 | `rgba(0,102,204,.1)` | Selected/active fills                              |
| `--color-success` (+`-subtle`)          | `#28a745`            | Running / healthy                                  |
| `--color-danger` (+`-hover`, `-subtle`) | `#dc3545`            | Errors, destructive actions                        |
| `--color-warning` (+`-subtle`)          | `#ffc107`            | Attention, degraded connection                     |

Status semantics (status is never conveyed by color alone — pair with text or a dot
plus label):

- **Running/healthy** → success on success-subtle fill
- **Stopped/idle** → muted text on surface-alt fill
- **Error** → danger on danger-subtle fill
- **Attention/disconnected** → warning, optionally with `pulse` animation

The page canvas is `--color-surface-alt`; content sits on `--color-surface` panels
bounded by `1px solid var(--color-border)`. That two-layer relationship (gray canvas,
white panels) is the basic visual texture of the app.

### Typography

Families: `--font-sans` (system stack) for all UI; `--font-mono` for terminal
content, branch/workspace names, paths, env values, and anything copy-pasteable.

| Role            | Size                    | Weight  | Notes                                                                                                                                 |
| --------------- | ----------------------- | ------- | ------------------------------------------------------------------------------------------------------------------------------------- |
| Page title      | `1rem`                  | 600     | `.app-header__meta` / `.config-sticky-header__title`. Page titles are deliberately quiet — this is a dense tool, not a marketing site |
| Section header  | `0.875rem`              | 600     | Uppercase + `letter-spacing: 0.05em` when used as a settings-section or sidebar group title                                           |
| Body / controls | `0.875rem`              | 400–500 | Buttons, inputs, labels, table cells                                                                                                  |
| Secondary text  | `0.8125rem` or `0.8rem` | 400     | Descriptions, item details                                                                                                            |
| Small / badges  | `0.75rem`               | 500     | Pills, badges, tabs, hints                                                                                                            |
| Tiny / metadata | `0.65rem`–`0.7rem`      | 400–500 | Sidebar workspace metadata, tab status lines                                                                                          |

Hero type (`1.75rem/700`) exists only on the Home page hero. Do not introduce new
sizes; if a value isn't in this table, round to the nearest one. `line-height: 1.5`
is the body default (set on `body`); only override for compact UI rows.

### Spacing

The scale is `--spacing-xxs/xs/sm/md/lg/xl/2xl` = 2/4/8/12/16/24/32 px. Recurring
applications:

- Page content padding: `--spacing-lg` (16px) inside panels, `--spacing-xl` (24px) page gutters
- Card header padding: `12px 16px` (`md` `lg`); card body: `16px` (`lg`)
- Vertical rhythm between sections: `md`–`lg`; between major columns: `xl`
- Inline gaps: `xs` (icon-to-label), `sm` (control groups), `md` (form rows)

Arbitrary pixel values (e.g. `margin-top: 13px`, `padding: 10px`) are non-compliant.

### Radii, shadows, motion

- Radii: `--radius-sm` 4px (tooltips, code chips), `--radius-md` 6px (buttons,
  inputs, list items), `--radius-lg` 8px (cards, modals, pills)
- Shadows: `--shadow-sm/md/lg`; `lg` is reserved for floating layers (modals,
  dropdowns)
- Motion: `--duration-fast` 150ms for hover/focus/most transitions,
  `--duration-normal` 250ms for layout/slide-in, easing `var(--easing)`
  (`cubic-bezier(0.4, 0, 0.2, 1)`). Dropdowns/tooltips animate in with a
  150ms fade + 2–4px translateY.

---

## 2. Page anatomy

### App shell

```
.app-shell (grid: 280px | 1fr, min-height 100vh)
├─ .app-shell__nav      sticky, 100vh, white, 1px right border
│   ├─ .nav-header          logo + collapse control
│   ├─ .nav-spawn-btn       primary "add workspace" action
│   ├─ .nav-workspaces      scrollable workspace list
│   ├─ .tools-section       collapsible tool links
│   └─ .sidebar-user        optional auth block (bottom)
└─ .app-shell__content  flex column; page renders here
```

Sidebar collapses to 48px (icon rail). Pages never reach into the shell; they own
only `.app-shell__content`.

### The three page templates

Every page is one of these. A page that is none of them is non-compliant by
composition.

**A. Workbench page** (session detail, spawn, diff, git graph):

```
.app-shell__content
├─ .app-header        workspace identity: branch, git status, actions
├─ .session-tabs      tab row (sessions + accessory tabs: diff, git, previews)
└─ [content]          flex:1, min-height:0 — terminal, spawn form, diff view
    └─ .spawn-content-style panel where applicable (white, 1px border,
       radius 0 0 8px 8px so it visually attaches to the tab row)
```

**B. Tool page** (environment, overlays, config, autolearn, repofeed, tips, events,
personas, timelapse…): same skeleton, but the page supplies its own header:

```
[page root]
├─ .app-header                      h1.app-header__meta title (1rem/600),
│                                   actions on the right
├─ optional tab bar (.wizard__steps) for multi-view pages
└─ content panel(s)                 white cards on the alt canvas
    ├─ table page → .env-table pattern inside one .spawn-content panel
    └─ settings page → stacked .settings-section cards in scrollable area
```

Environment (`EnvironmentPage.tsx:100`) is the canonical minimal example — it reuses
the same `.app-header` the workbench uses, which is exactly why it feels native.
Config adds the sticky header + `.wizard__steps` tab bar for multi-tab pages.

**C. Full-bleed page** (preview iframes, timelapse player): no chrome beyond a
minimal header; content fills the viewport. Use sparingly.

### In-page tabs

The standard tab bar is `.wizard__steps`: full-width row, `--color-surface-alt`
background, 1px bottom border; each `.wizard__step` is `12px 16px`, `0.875rem/500`,
muted color, transparent 2px bottom border; active = accent text + accent bottom
border. Tips, build-status views, and any future multi-view tool page must use this
pattern (or `.session-tabs` if the tabs represent live sessions), not bespoke pills
or sidebars.

A `.wizard__steps` bar only reads as tabs when a content panel attaches directly
beneath it — the white panel edge is what defines the gray strip against the gray
canvas. Attach the panel flush (no gap; `border-radius: 0 0 var(--radius-lg)
var(--radius-lg)`), exactly as the workbench's `.spawn-content` attaches to its tab
row. A tab strip floating on the bare canvas with no attached panel is
non-compliant — it disappears.

**Nested sub-tabs.** A page may have at most **two** tab levels; a third axis is a
filter or dropdown, never a third tab bar. When a primary view must switch between
peer datasets of the same kind (e.g. Overlays' per-repo selection), the inner
selector is `.wizard__steps--nested` — the same tab family, demoted to the panel's
top row:

- Placement: the **first row inside** the content panel (like a card header), not a
  second strip on the canvas. The primary bar stays outside, with the panel
  attached to it.
- Background `--color-surface` (white, matching the panel); `1px solid
var(--color-border-subtle)` bottom hairline; steps `8px 12px`, `0.75rem/500`.

White-inside-white, smaller type, and a hairline keep the nested row subordinate
even at equal width — the failure mode is a same-width gray bar floating between
the parent and the panel, which reads as broken layout. Nest **only** for peer
views of the same kind; if the inner axis merely filters otherwise-identical
content, use a segmented control or a header dropdown, not a sub-tab.

### Sections within a page

Settings-style content uses `.settings-section`:

- container: surface-alt bg, 1px border, `--radius-lg`, `margin-bottom: var(--spacing-md)`
- `__header`: `12px 16px`, surface bg, 1px bottom border
- `__title`: `0.875rem/600`, uppercase, `letter-spacing: 0.05em`
- `__body`: `16px`

Dashboard-style content (Home, and any grid or list of content/entity cards) uses the
equivalent card pattern (`.card` / `.sectionCard`): white bg, 1px border,
`--radius-lg`, header `12px 16px` on surface-alt. The two are the same shape with
inverted fills.

**Which fill, by context.** A panel or card that rests directly on the gray canvas
must read as white-on-gray (§6 dim 1): use the white `.card` fill (`--color-surface`).
This covers free-standing content panels and every grid or list of entity cards
(personas, styles, repos, …). The inverted `.settings-section` fill (gray
`--color-surface-alt` body) is reserved for stacked settings groups, where the white
`__header` strip is what lifts them off the canvas. A card that is entirely
`--color-surface-alt` with only a 1px border — gray-on-gray, no white surface — is
non-compliant; the border alone does not separate it from the canvas. Bare `<h3>`
headings floating over unwrapped content are non-compliant.

---

## 3. Component conventions

### Buttons

Base `.btn`: `8px 16px`, `--radius-md`, `0.875rem/500`, inline-flex with `gap:
var(--spacing-sm)`, transition `all var(--duration-fast) var(--easing)`.

| Variant                    | Resting                        | Hover                           |
| -------------------------- | ------------------------------ | ------------------------------- |
| `.btn` (default)           | surface-alt bg                 | border-color bg                 |
| `.btn--primary`            | accent bg, white text          | accent-hover bg                 |
| `.btn--secondary`          | transparent, 1px border        | surface-alt bg, muted border    |
| `.btn--danger`             | danger bg, white text          | danger-hover bg                 |
| `.btn--ghost`              | transparent                    | surface-alt bg                  |
| `.btn--bordered`           | adds 1px border to any variant | muted border                    |
| `.btn--sm`                 | `4px 12px`, `0.75rem`          | —                               |
| `.btn--icon` / `.icon-btn` | `8px`, muted color             | surface-alt bg, full-color icon |

Disabled: `opacity: 0.5; cursor: not-allowed`.

**Which variant, by role** (derived from where the reference surfaces use each):

- **`--primary`** marks the action that accomplishes its container's purpose:
  a form's submit (PersonaForm, AddRepoModal), a dialog's confirm
  (ModalProvider), a row's state-changing action (Environment's per-row Sync).
  One per container — the container may be a page, dialog, card, or row. A
  toggle that is ON renders as primary (SessionDetailPage's local-echo).
- **`--secondary`**, or equivalently **`--ghost --bordered`** (same outline
  silhouette): utility actions — export, diagnose, copy, open-in-editor — on
  rows, toolbars, and panel headers (WorkspaceHeader, TimelapsePage rows,
  metrics panels).
- **`--ghost`** alone: inline actions inside dense chrome where a box would add
  noise — a tab's dispose ✕ (SessionTabs), a panel footer's Clear. Pairs with
  `--danger` for quiet destructive actions.
- **`--danger`** solid: destructive actions standing on their own (delete a
  recording); always behind a confirm (product rule: "destructive actions
  slow").
- **`.icon-btn`**: icon-only utilities in the shell and row chrome.

Every button is `.btn` plus modifiers from this table. A new need means a new
shared modifier in `global.css`.

**Variants are dimensionally interchangeable.** The base `.btn` reserves a `1px solid
transparent` border that variants only recolor — they never add or remove the border.
So swapping a button between variants (a toggle rendering as `--primary` when on and
`--secondary` when off; a filter chip flipping active/inactive) never changes its box
or nudges the layout around it. A variant that adds or drops a border instead of
recoloring the reserved one will resize the button and bounce its neighbors.

### Iconography

All iconography is the shared stroke-SVG set: `Icons.tsx` and the inline
24×24 stroke icons used by the sidebar (`ToolsSection.tsx`) — sized to the text
they accompany, colored via `currentColor`, always paired with a text label or
`aria-label`. Section headers are plain text. Status is communicated by
status-pill dots and text. Emoji do not appear in UI chrome — labels, headers,
buttons, badges.

### Charts and data-viz color (SVG)

Histograms, the commit graph, and any other SVG/canvas visual take their colors
from tokens like everything else — but a token reaches SVG through the CSS
`fill`/`stroke` **property**, never the `fill=`/`stroke=` presentation attribute.
`var()` is not substituted inside a presentation attribute: `<rect
fill="var(--color-success)">` renders the default (black), not the token. (Several
existing SVGs pass `var()` strings straight to `fill=` — `commitGraphLayout.ts`'s
`GRAPH_COLOR`, the metrics histograms; treat those as suspect until confirmed in
the browser, not as the pattern to copy.)

- Fixed colors → a CSS class carrying `fill: var(--token)` / `stroke: var(--token)`.
- Data-driven, per-element colors (e.g. histogram bars keyed by value) → set the
  CSS property inline: `style={{ fill: 'var(--color-success)' }}`. This is one of
  the sanctioned dynamic-inline cases (§5) — the value is still a token, never a hex.

Keep a data-viz ramp on tokens that stay distinct in both themes — e.g. success /
warning / `--color-graph-lane-3` / danger for a four-step severity scale.

### Forms

`.form-group` per field: `__label` (`0.875rem/500`, `margin-bottom: var(--spacing-sm)`),
control, then `__hint` (`0.75rem`, faint) or `__error` (`0.75rem`, danger).
Inputs/selects/textareas: `8px 12px`, 1px border, `--radius-md`, `0.875rem`,
focus = accent border + `0 0 0 3px var(--color-accent-subtle)` ring (no outline).
Collections of editable items use `.item-list`; add-forms use `.add-item-form`.
Lists of toggleable options (e.g. selecting which repos a feature applies to) use
`.checkbox-list` with `.checkbox-list__item` rows (a checkbox plus a `<span>` label).

### Status pills, badges, dots

`.status-pill`: inline-flex, `4px 12px`, `--radius-lg`, `0.75rem/500`, 6px round
`__dot`, variants `--running/--stopped/--error` per the status semantics above.
`.badge`: `4px 12px`, `--radius-lg`, `0.75rem/500`. Tiny indicator badges
(`.badge--indicator`) are `2px 6px`, `0.65rem`. New status visuals must be a variant
of these, not new shapes.

### Tables

Semantic `<table>` with `<thead>`: header cells `0.75rem`, uppercase,
`letter-spacing: 0.05em`, muted; rows separated by `--color-border-subtle`; row
hover surface-alt; monospace for identifier columns. `.env-table` and
`.session-table` are the references.

### Modals, dropdowns, tooltips, toasts

- Modal: overlay `rgba(0,0,0,0.5)`; panel = surface bg, 1px border, `--radius-lg`,
  `--shadow-lg`, max-width 440px (640px wide variant); header/body/footer each pad
  `--spacing-lg` with hairline separators; footer right-aligns buttons. Esc closes;
  focus is trapped. Always via `ModalProvider`.
- Dropdown menus: `ActionDropdown` pattern — elevated surface, 1px border, radius
  8px, `--shadow-lg`-class shadow, 150ms fade-slide, items `8px 12px`/`0.85rem`,
  hover surface-alt.
- Tooltips: `Tooltip` component (`.tooltip-react`) — mono `0.75rem`, inverted
  colors, `--radius-sm`, 150ms fade.

### Empty and loading states

Empty: `.empty-state` / `.empty-state-hint` — centered or inline muted text, no
bespoke illustrations. Loading: existing spinner classes (`spin` 0.8s linear);
skeletons only where they already exist.

### Scrollbars

Scrollable regions opt in to the thin scrollbar styling (8px, `--color-border`
thumb, transparent track) by being listed with the existing scoped selectors —
never style scrollbars globally (it bleeds into xterm).

---

## 4. Interaction and accessibility

- Hover feedback on every interactive element: background shift to surface-alt (or
  the variant hover token), `--duration-fast`.
- Focus: `:focus-visible` outline `2px solid var(--color-accent)`, offset 2px
  (−2px inside dropdowns/modals). Inputs use the border+ring treatment instead.
- All icon-only buttons have `aria-label`; dialogs trap focus; color is never the
  only signal.
- Respect the live-data rules from `docs/web.md`: don't reorder or collapse content
  under the user, preserve scroll position.

---

## 5. CSS architecture and naming

Two sanctioned homes for styles, both consuming the same tokens:

1. **`global.css`** — design tokens, shared primitives (`.btn`, `.form-group`,
   `.settings-section`, `.status-pill`, `.modal`, `.app-header`, `.wizard__*`,
   `.item-list`, `.checkbox-list`, `.env-table`, …) and existing page sections. Naming:
   **kebab-case BEM-ish** — `block__element--modifier` (`.session-tab__name`,
   `.btn--primary`, `.env-badge--in-sync`).
2. **CSS modules** (`*.module.css`) — page- or component-scoped styles that aren't
   reusable primitives. Naming: **camelCase** flat (`.sectionCard`, `.branchRow1`).
   Modules must still use `var(--*)` tokens exclusively; a module with hardcoded
   hex values is the #1 dark-mode breaker.

Rules of placement:

- Reusable pattern → `global.css` primitive (or extend one). Page-specific layout →
  that page's module. Don't fork a primitive into a module to tweak it; add a
  modifier.
- Prefer page modules for new page work — `global.css` is already ~7,100 lines;
  modules keep diffs isolated and reviewable.
- **Inline `style=` attributes are reserved for genuinely dynamic values**
  (computed positions, data-driven colors, drag offsets). Static layout in inline
  styles is non-compliant. (Current worst offender: `IOWorkspaceMetricsPanel.tsx`.)
- Utility classes that exist (`.mb-md`, `.text-muted`, `.flex-row`, `.gap-sm`, …)
  may be used for one-off spacing/alignment instead of inventing single-purpose
  classes.

---

## 6. Compliance rubric

Score each page on these seven dimensions (use this list both to evaluate an existing
surface and as the done-criteria for new or changed UI):

1. **Composition** — uses one of the three page templates; correct header pattern; white-panels-on-gray-canvas layering
2. **Typography** — sizes/weights from the scale; 1rem/600 page title; uppercase section-header treatment; mono where copy-pasteable
3. **Color** — tokens only; works in dark mode; status semantics correct
4. **Spacing** — token scale only; standard card/panel padding; standard rhythm
5. **UI conventions** — standard tabs, forms, tables, pills, modals, empty states; no bespoke parallels
6. **Interaction** — button variants + hover/focus/disabled effects; standard transitions
7. **Naming/architecture** — BEM-ish kebab in global.css, camelCase in modules, tokens in both, no static inline styles

A page is compliant when a screenshot of it could be mistaken for part of the
session/spawn/config family, in both themes.

Scoring is a human visual pass: a reviewer opens the surface in both themes (the
in-app Toggle-theme button flips light/dark) and judges it against the seven
dimensions. It is not something a tool or an automated agent self-certifies —
there is no substitute for looking. An agent converting a surface produces the
concrete "where to look / what to expect" recipe the reviewer runs; it does not
score the rubric itself.
