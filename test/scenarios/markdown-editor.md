# Edit a Markdown file while an agent changes it

A user opens a Markdown file from a local workspace, types into it, and sees
the change land on disk without a Save button. An agent then rewrites part of
the file on disk; the editor shows the agent's change without a reload, and
when both sides change the same line the file ends up containing both edits.

## Preconditions

- The dashboard is running with a local git workspace that contains `docs/notes.md`
  with the three lines `alpha`, `beta`, `gamma`.
- No agent session is required; "the agent" is the test writing the file on disk.

## Verifications

- Opening `/diff/{workspaceId}/md/docs%2Fnotes.md` renders the editor (`data-testid="markdown-editor"`)
  with the file's text and a status of `Saved`.
- Typing ` one` at the end of the first line changes the status to `Saving…` and then `Saved`,
  and the file on disk reads `alpha one`, `beta`, `gamma`.
- Writing `alpha one`, `beta`, `gamma two` to the file on disk (atomic rename, the way `sed -i` does)
  updates the editor's third line to `gamma two` without reload, and the status stays `Saved`.
- Typing ` three` at the end of the second line while, before autosave fires, the test rewrites the
  same line on disk as `beta four`, results in one file whose second line contains both `three`
  and `four`, and the editor shows that merged line.
- The Download link points at `/api/file/{workspaceId}/docs%2Fnotes.md`.
