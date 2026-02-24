# GitHub Auth Config UI Fix

## Problem

When editing GitHub OAuth credentials in the config modal:

1. Client ID shows blank instead of the actual value
2. Client Secret shows blank, giving no indication a secret exists

This is confusing because users can't see what's configured and must re-enter both values every time.

## Solution

### Backend Changes

**`GET /api/auth/secrets`** (`internal/dashboard/handlers_config.go`):

Return the actual `client_id` value (it's not a secret - visible in GitHub OAuth app settings):

```json
{
  "client_id": "Ov23liXyZ123",
  "client_secret_set": true
}
```

**`POST /api/auth/secrets`**:

Modify to support partial updates:

- If `client_secret` is omitted or empty, keep existing secret unchanged
- Still requires both values if no secret exists yet (validation)

### Frontend Changes

**Modal State** (`assets/dashboard/src/routes/config/useConfigForm.ts`):

Extend `AuthSecretsModalState` to track whether secret was previously set:

```typescript
type AuthSecretsModalState = {
  clientId: string;
  clientSecret: string;
  clientSecretWasSet: boolean; // new field
  error: string;
} | null;
```

**Modal Behavior** (`assets/dashboard/src/routes/config/ConfigModals.tsx`):

| Field         | When secret NOT set | When secret IS set |
| ------------- | ------------------- | ------------------ |
| Client ID     | Blank               | Shows actual value |
| Client Secret | Blank               | Shows `••••••••`   |

**Secret Field Interaction:**

- On focus: clears mask to blank (ready for new input)
- Placeholder text when secret exists: "Enter new secret (leave blank to keep existing)"

**Opening the Modal** (`assets/dashboard/src/routes/ConfigPage.tsx`):

```typescript
const openAuthSecretsModal = async () => {
  const status = await getAuthSecretsStatus();
  dispatch({
    type: 'SET_AUTH_SECRETS_MODAL',
    modal: {
      clientId: status.client_id || '',
      clientSecret: status.client_secret_set ? '••••••••' : '',
      clientSecretWasSet: status.client_secret_set,
      error: '',
    },
  });
};
```

**Save Logic:**

```typescript
const saveAuthSecretsModal = async () => {
  const { clientId, clientSecret, clientSecretWasSet } = state.authSecretsModal;

  // Validate client ID
  if (!clientId.trim()) {
    // error: client ID required
    return;
  }

  // Validate client secret
  if (!clientSecretWasSet && !clientSecret.trim()) {
    // error: client secret required (first time setup)
    return;
  }

  // Build request - only include secret if user entered a new value
  const request: { client_id: string; client_secret?: string } = {
    client_id: clientId.trim(),
  };

  if (clientSecret.trim() && clientSecret !== '••••••••') {
    request.client_secret = clientSecret.trim();
  }

  await saveAuthSecrets(request);
  // ... success handling
};
```

## API Contract

### GET /api/auth/secrets

**Response:**

```json
{
  "client_id": "string",        // actual value, empty string if not set
  "client_secret_set": boolean  // true if secret exists
}
```

### POST /api/auth/secrets

**Request:**

```json
{
  "client_id": "string", // required
  "client_secret": "string" // optional; if omitted/empty, keep existing
}
```

**Behavior:**

- If no secret exists and `client_secret` is empty/omitted → 400 error
- If secret exists and `client_secret` is empty/omitted → keep existing, update client_id only
- If `client_secret` has value → update secret

## Files to Modify

1. `internal/dashboard/handlers_config.go` - Update GET/POST handlers
2. `assets/dashboard/src/routes/config/useConfigForm.ts` - Extend modal state type
3. `assets/dashboard/src/routes/config/ConfigModals.tsx` - Mask display, clear on focus
4. `assets/dashboard/src/routes/ConfigPage.tsx` - Fetch actual ID, save logic
5. `assets/dashboard/src/lib/api.ts` - Update `getAuthSecretsStatus` return type
6. `assets/dashboard/src/lib/types.ts` - Update API types if needed

## Testing

- Test opening modal with no credentials set (blank fields)
- Test opening modal with credentials set (ID shown, secret masked)
- Test clearing secret field and saving (keeps existing)
- Test entering new secret and saving (updates)
- Test first-time setup without secret (validation error)
