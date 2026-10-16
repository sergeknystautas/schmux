# Plan Usage Provider Links Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make each listed provider name in the sidebar's Plan Usage panel open that provider's usage page in a new tab.

**Architecture:** A frontend-only change to `PlanUsagePanel.tsx`. A `PROVIDER_USAGE_URLS` map sits next to the existing `PROVIDER_NAMES` map, keyed by the same provider id. `ProviderCard` wraps the name in an anchor when the id has a URL and leaves it as plain text otherwise. A `.plan-usage__link` class keeps the anchor looking like today's name at rest and shows link styling on hover, following the neighboring `.event-monitor__link` rule.

**Tech Stack:** React, TypeScript, Vitest + React Testing Library, global CSS.

**Spec:** `docs/superpowers/specs/2026-10-11-plan-usage-provider-links-design.md`

## Spike

Question: Will an `<a>` inside `.plan-usage__name` look like today's plain name without its own CSS?
Answer: No. The global rule `a { color: var(--color-accent); }` (`assets/dashboard/src/styles/global.css:3938-3940`) makes every anchor accent-colored, and `a:hover` adds an underline (`:3942-3944`). The anchor needs a class that sets `color: inherit` at rest. That is why Task 1 includes the `.plan-usage__link` CSS.

## Global Constraints

- Usage pages, verbatim from the spec:
  - Claude (`anthropic`): `https://claude.ai/settings/usage`
  - Codex (`openai`): `https://chatgpt.com/settings/usage`
  - Kimi (`moonshot`): `https://www.kimi.ai/code/console`
  - GLM (`zai`): `https://z.ai/manage-apikey/coding-plan/personal/usage`
  - MiniMax (`minimax`): `https://platform.minimax.io/console/usage`
- Clicking a provider name opens its usage page in a new tab.
- At rest, the name looks as it does today: quiet, no link styling.
- On hover, the name shows link styling: accent color, underline, pointer cursor.
- The names are not keyboard tab stops.
- A provider not in the table keeps its plain-text name, with no link.
- No backend, API contract, or generated-type changes.
- No changes to provider usage calculations or the panel's polling and collapse behavior.
- Run tests only through `./test.sh` from the repository root. Never run `npx vitest` directly.
- Commit only through `/commit`. Never run `git commit` directly.

## Review Focus

1. A provider id with no URL entry, such as a raw id like `custom` → the name renders as plain text with no link. (Task 1, Step 1, second test)
2. Keyboard Tab through the sidebar → focus skips the provider names. (Task 1, Step 1, first test asserts `tabindex="-1"`)
3. A visited usage page → the name at rest still looks like the other names, not accent or purple. `color: inherit` on `.plan-usage__link` covers both. jsdom doesn't load `global.css`, so this is a manual check in Task 1, Step 7.
4. Clicking a name → it opens a new tab and doesn't collapse the panel. The anchor sits outside the collapse `<button>` (`PlanUsagePanel.tsx:197`), so no code path joins them. This is a manual check in Task 1, Step 7.
5. Existing text queries such as `getByText('Codex')` and `getByText('Kimi')` → they still match because the text node only moves inside an anchor. The existing suite run in Task 1, Step 4 covers this.

---

### Task 1: Link provider names to their usage pages

**Files:**

- Modify: `assets/dashboard/src/components/PlanUsagePanel.tsx:5-11` (add the URL map below `PROVIDER_NAMES`)
- Modify: `assets/dashboard/src/components/PlanUsagePanel.tsx:137` (the name element in `ProviderCard`)
- Modify: `assets/dashboard/src/styles/global.css:426-430` (add `.plan-usage__link` rules after `.plan-usage__name`)
- Modify: `docs/plan-usage.md:25`, `docs/plan-usage.md:80`
- Test: `assets/dashboard/src/components/PlanUsagePanel.test.tsx`

**Interfaces:**

- Consumes: the existing `report(windows, provider)` and `show()` helpers in `PlanUsagePanel.test.tsx:10-29`.
- Produces: the module-private `PROVIDER_USAGE_URLS: Record<string, string>` in `PlanUsagePanel.tsx`, and the CSS class `plan-usage__link`.

- [ ] **Step 1: Write the failing tests**

Append to `assets/dashboard/src/components/PlanUsagePanel.test.tsx`:

```tsx
test.each([
  ['anthropic', 'Claude', 'https://claude.ai/settings/usage'],
  ['openai', 'Codex', 'https://chatgpt.com/settings/usage'],
  ['moonshot', 'Kimi', 'https://www.kimi.ai/code/console'],
  ['zai', 'GLM', 'https://z.ai/manage-apikey/coding-plan/personal/usage'],
  ['minimax', 'MiniMax', 'https://platform.minimax.io/console/usage'],
])('links provider %s name %s to its usage page in a new tab', async (id, name, url) => {
  vi.mocked(getUsage).mockResolvedValue(
    report(
      [{ id: 'primary', used_percent: 29, duration_minutes: 10080, resets_at: resetIn(84) }],
      id
    )
  );
  await show();
  const link = screen.getByRole('link', { name });
  expect(link).toHaveAttribute('href', url);
  expect(link).toHaveAttribute('target', '_blank');
  expect(link).toHaveAttribute('rel', 'noopener noreferrer');
  expect(link).toHaveAttribute('tabindex', '-1');
});

test('an unknown provider keeps its plain-text name with no link', async () => {
  vi.mocked(getUsage).mockResolvedValue(
    report(
      [{ id: 'primary', used_percent: 29, duration_minutes: 10080, resets_at: resetIn(84) }],
      'custom'
    )
  );
  await show();
  expect(screen.getByText('custom')).toBeInTheDocument();
  expect(screen.queryByRole('link')).not.toBeInTheDocument();
});
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `./test.sh --frontend --run 'links provider'`
Expected: 5 FAIL with `Unable to find an accessible element with the role "link"`.

Run: `./test.sh --frontend --run 'unknown provider keeps its plain-text name'`
Expected: PASS. It already passes because no names are links yet. It guards the fallback once Step 3 adds links.

- [ ] **Step 3: Write the implementation**

In `assets/dashboard/src/components/PlanUsagePanel.tsx`, add this directly below `PROVIDER_NAMES`:

```tsx
const PROVIDER_USAGE_URLS: Record<string, string> = {
  anthropic: 'https://claude.ai/settings/usage',
  openai: 'https://chatgpt.com/settings/usage',
  moonshot: 'https://www.kimi.ai/code/console',
  zai: 'https://z.ai/manage-apikey/coding-plan/personal/usage',
  minimax: 'https://platform.minimax.io/console/usage',
};
```

In `ProviderCard`, replace

```tsx
<div className="plan-usage__name">{providerName(provider.provider)}</div>
```

with

```tsx
<div className="plan-usage__name">
  {usageUrl ? (
    <a
      className="plan-usage__link"
      href={usageUrl}
      target="_blank"
      rel="noopener noreferrer"
      tabIndex={-1}
    >
      {providerName(provider.provider)}
    </a>
  ) : (
    providerName(provider.provider)
  )}
</div>
```

and add this as the first line of the `ProviderCard` body, above `const displayable = ...`:

```tsx
const usageUrl = PROVIDER_USAGE_URLS[provider.provider];
```

In `assets/dashboard/src/styles/global.css`, add these rules directly after the `.plan-usage__name { ... }` block (`:426-430`):

```css
.plan-usage__link {
  color: inherit;
  text-decoration: none;
}

.plan-usage__link:hover {
  color: var(--color-accent);
  text-decoration: underline;
}
```

The browser gives an anchor with `href` a pointer cursor by default, so no `cursor` rule is needed.

- [ ] **Step 4: Run the panel's tests to verify they pass**

Run: `./test.sh --frontend --run 'links provider'`
Expected: 5 PASS.

Run: `./test.sh --frontend`
Expected: all frontend tests PASS, including the existing `PlanUsagePanel.test.tsx` cases and `getByText('Codex')`/`getByText('Kimi')`.

- [ ] **Step 5: Update the subsystem doc**

In `docs/plan-usage.md`, change the `PlanUsagePanel.tsx` row in the Key files table (`:25`) from:

```
| `assets/dashboard/src/components/PlanUsagePanel.tsx`      | Sidebar panel: provider label, reserve/deficit, time left                                   |
```

to:

```
| `assets/dashboard/src/components/PlanUsagePanel.tsx`      | Sidebar panel: provider label linked to its usage page, reserve/deficit, time left          |
```

Change the provider display name bullet (`:80`) from:

```
- **To add a new provider display name:** extend `PROVIDER_NAMES` in `PlanUsagePanel.tsx`. No backend change needed — the panel receives the resolved provider id.
```

to:

```
- **To add a new provider display name:** extend `PROVIDER_NAMES` in `PlanUsagePanel.tsx`. No backend change needed — the panel receives the resolved provider id. To link the name to the provider's usage page, add the same id to `PROVIDER_USAGE_URLS`; an id without a URL renders as plain text. These URLs are separate from the profile `UsageURL` field in `internal/models/profiles.go`, which is not sent to the dashboard.
```

Then run `./format.sh` so Prettier realigns the table.

- [ ] **Step 6: Run the dashboard style check**

Invoke the `schmux-dashboard-style-check` skill on the `global.css` and `PlanUsagePanel.tsx` changes. Expected: no hardcoded colors and no undefined tokens. `--color-accent` is defined (`docs/dashboard-style-guide.md:77`).

- [ ] **Step 7: Ask the user for a manual check**

Ask the user to check in their running dashboard. Do not start or restart the dev server.

- At rest, every provider name looks like before: faint, not underlined. This includes names whose usage page was already visited.
- On hover, the name turns accent-colored and underlined, with a pointer cursor.
- Clicking a name opens the usage page in a new tab, and the panel stays expanded.
- Pressing Tab through the sidebar skips the provider names.

- [ ] **Step 8: Commit**

Run `/commit`. Message: `feat(plan-usage): link provider names to their usage pages`.

### Task 2: Finalize the spec and remove the plan

**Files:**

- Delete: `docs/superpowers/specs/2026-10-11-plan-usage-provider-links-design.md`
- Delete: `docs/superpowers/plans/2026-10-15-plan-usage-provider-links.md`
- Modify: `docs/plan-usage.md` (only if `/finalize` finds a spec decision Task 1, Step 5 didn't capture)

**Interfaces:**

- Consumes: Task 1's merged behavior and doc edits.
- Produces: nothing.

- [ ] **Step 1: Run `/finalize`**

Run `/finalize` on `docs/superpowers/specs/2026-10-11-plan-usage-provider-links-design.md`. Fold any behavior the spec states that `docs/plan-usage.md` doesn't yet say into that doc, such as "names are not keyboard tab stops" or "unknown providers stay plain text". Delete the spec and this plan.

- [ ] **Step 2: Verify**

Run: `./test.sh`
Expected: all suites PASS.

Run: `./badcode.sh`
Expected: exit 0.

Run: `ls docs/superpowers/specs/2026-10-11-plan-usage-provider-links-design.md docs/superpowers/plans/2026-10-15-plan-usage-provider-links.md`
Expected: `No such file or directory` for both.

- [ ] **Step 3: Commit**

Run `/commit`. Message: `docs(plan-usage): finalize provider links spec`.
