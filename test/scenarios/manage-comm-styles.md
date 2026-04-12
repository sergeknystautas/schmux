# Manage communication styles

A user wants to browse, create, edit, and delete communication styles.
They navigate to the Comm Styles page via the sidebar, browse the built-in
styles, create a custom style, edit it, then delete it. Built-in styles
can be "deleted" which resets them to their defaults.

## Preconditions

- The daemon is running
- Comm Styles experimental feature is enabled via PUT /api/config { comm_styles_enabled: true }

## Verifications

- The sidebar shows a "Comm Styles" link
- Clicking "Comm Styles" navigates to /styles
- The styles page shows at least 25 built-in style cards
- Each style card shows an icon, name, and tagline
- Clicking "Create Style" navigates to /styles/create
- Filling in name, icon, and prompt fields and clicking "Create" succeeds
- The new custom style appears in the list at /styles
- GET /api/styles returns the custom style in the response
- Clicking "Edit" on the custom style navigates to /styles/{id}
- Changing the tagline and clicking "Save Changes" succeeds
- GET /api/styles/{id} returns the updated tagline
- Deleting the custom style removes it from the list
- GET /api/styles no longer includes the deleted custom style
- Deleting a built-in style resets it (it still appears in the list)
