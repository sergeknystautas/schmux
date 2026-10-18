---
name: dashboard-style-check
description: Use when adding or changing React/CSS markup in the schmux web dashboard (`assets/dashboard/src/**`) before presenting the work. Triggers on new pages, components, tabs, forms, tables, modals, status visuals, and any CSS or CSS module that touches colors, spacing, type, or layout. Catches the most common compliance failures the `docs/dashboard-style-guide.md` rubric exists for — bare `<input>`/`<select>`/`<textarea>`/`<button>` rendering unstyled, hardcoded colors breaking dark mode, SVG `fill="var(--*)"` presentation attributes that don't substitute, tab strips floating without an attached panel, and bespoke parallels to primitives that already exist.
source: emerged
triggers:
  - 'before presenting new or changed dashboard UI'
  - 'after writing CSS or a CSS module for a new page or component'
  - 'when adding a new control, badge, dropdown, modal, tab, or status visual'
---

## Overview

The dashboard's `global.css` has **no bare element rules** for `input`, `select`, `textarea`, or `button`. An unclassed `<button>` is not unstyled — it is broken (black text on light gray in light theme, black on near-black in dark theme). Most compliance failures are hand-rolled markup that duplicates a primitive already defined in the guide. This skill is the pre-flight checklist against `docs/dashboard-style-guide.md`; the guide is the source of truth, this skill is the runnable enforcement.

## Output contract

When you finish dashboard UI work and before you present it:

1. Walk the checklist below in order. For every item: **pass**, **fix** (with `path:line`), or **escalate** (ask the reviewer).
2. Present the result as a short `Style check` block with each item's status. If anything is a **fix**, apply it and re-run the checklist before presenting.
3. Do not self-certify "this looks fine" — present the checklist so the reviewer runs the visual pass against the same list.

## Checklist

### Composition

- [ ] Uses one of the three page templates (workbench / tool / full-bleed) — guide §2.
- [ ] White panels on gray canvas: panel uses `--color-surface` on `--color-surface-alt`. A gray-on-gray card (no white fill) is non-compliant.
- [ ] Page header is `.app-header__meta` (1rem/600). No bare `<h3>` over unwrapped content.
- [ ] Tab strip (`.wizard__steps` / `.session-tabs`) attaches flush to a panel (`border-radius: 0 0 var(--radius-lg) var(--radius-lg)`, no gap). A floating strip with no attached panel disappears.
- [ ] Settings content uses `.settings-section`; entity grids/lists use `.card` / `.sectionCard`.

### Typography

- [ ] Sizes/weights come from the table in guide §1. No new values; round to nearest. Page title is 1rem/600.
- [ ] Mono (`--font-mono`) for terminal, branch/workspace names, paths, copy-pasteable values.
- [ ] Body `line-height: 1.5` (inherited from `body`); override only for compact UI rows.

### Color (tokens only)

- [ ] No hardcoded hex / rgb / hsl in CSS or JSX except the sanctioned list in guide §1 (terminal-emulator themes, third-party brand colors, user-data colors, backburner lavender `#b8a9e0`, vote gold `#f5c518`).
- [ ] SVG fills/strokes: use the **CSS `fill:` / `stroke:` property** (class or inline style) — never the `fill=` / `stroke=` HTML presentation attribute. `var()` does not substitute inside a presentation attribute.
- [ ] Status pairings: success/danger/warning each come with a `-subtle` fill and a text label or dot. Color is never the only signal.
- [ ] Verify in **both** light and dark themes (the in-app Toggle-theme button).

### Spacing

- [ ] Spacing uses `--spacing-xxs/xs/sm/md/lg/xl/2xl` tokens only. No arbitrary pixel values.
- [ ] Card / panel padding: `12px 16px` header, `16px` body. Page padding: `--spacing-lg` inside panels, `--spacing-xl` page gutters.

### UI conventions

- [ ] Every `<button>` has a `.btn` variant. Unclassed buttons render broken chrome.
- [ ] Every `<input>` is `.input`; `<select>` is `.select`; `<textarea>` is `.textarea`. Unclassed controls render unstyled.
- [ ] Copy control is `CopyButton` (or `.copy-field` with `className="copy-field__btn"`); never a hand-built copy button.
- [ ] Choice groups: row of `.btn--sm` (`--primary` selected / `--secondary` unselected, `aria-pressed`); lay out with `.btn-group`. Do not invent chip / pill / option-button classes.
- [ ] Modal goes through `ModalProvider`; dropdowns use `ActionDropdown`; tooltips use `Tooltip`; status visuals use `.status-pill` variants (`--running`/`--stopped`/`--error`); tabs use `.wizard__steps` or `.session-tabs`.

### Interaction

- [ ] Every interactive control has hover feedback (surface-alt or variant hover token) and `--duration-fast` transition.
- [ ] `:focus-visible` outline `2px solid var(--color-accent)`, offset 2px (−2px in dropdowns/modals). Inputs use border+ring instead.
- [ ] Icon-only buttons have `aria-label`; dialogs trap focus.
- [ ] Destructive actions confirm (product rule).

### Naming / architecture

- [ ] Reusable primitives live in `global.css` (kebab-case BEM-ish). Page-specific layout lives in a CSS module (camelCase flat, tokens via `var(--*)`).
- [ ] Inline `style=` only for genuinely dynamic values (computed positions, data-driven tokens via `style={{ fill: 'var(--color-…)' }}`). Static layout in inline styles is non-compliant.
- [ ] Reusable utilities (`.mb-md`, `.text-muted`, `.flex-row`, `.gap-sm`, …) used before inventing new helpers.

## Common misses

- Bare `<button>` rendered as a "small icon" — failure is not stylistic: it renders the browser default face.
- `<svg><rect fill="var(--color-success)" /></svg>` — black rect, every time. Move to `style={{ fill: 'var(--color-success)' }}` on the element.
- A new `.input`/`.select`/`.textarea` forgotten on a form control.
- A new `.chip` / `.option` / `.pill-btn` that duplicates `.btn--sm` choice-group or `.status-pill`.
- Status conveyed by color alone (no text, no dot).
- A `<div>` panel using `--color-surface-alt` with a 1px border — gray-on-gray; the panel disappears against the canvas. Use white surface.
- A `.wizard__steps` row separated from its content by a gap — looks like two unrelated strips; collapse the gap and attach the panel flush.

## Reference

Seven-dimension rubric, full token and primitive tables, and the page anatomy: [`docs/dashboard-style-guide.md`](../../../docs/dashboard-style-guide.md). Treat this skill as the runnable enforcement of that guide; update both when a token, primitive, or template changes.
