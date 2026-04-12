import { useCallback, useRef } from 'react';
import { updateConfig, getConfig, getErrorMessage } from '../../lib/api';
import { useConfig } from '../../contexts/ConfigContext';
import { useToast } from '../../components/ToastProvider';
import { CONFIG_UPDATED_KEY } from '../../lib/constants';
import { buildConfigUpdate } from './buildConfigUpdate';
import type { ConfigFormState, ConfigFormAction } from './useConfigForm';

export type SaveStatus = 'idle' | 'saving' | 'saved' | 'error';

/** Actions that always trigger auto-save when dispatched. */
const SAVE_TRIGGER_ACTIONS = new Set([
  'TOGGLE_MODEL',
  'CHANGE_RUNNER',
  'ADD_REPO',
  'REMOVE_REPO',
  'ADD_COMMAND_TARGET',
  'REMOVE_COMMAND_TARGET',
  'ADD_QUICK_LAUNCH',
  'REMOVE_QUICK_LAUNCH',
  'ADD_PASTEBIN',
  'REMOVE_PASTEBIN',
  'ADD_DIFF_COMMAND',
  'REMOVE_DIFF_COMMAND',
]);

/**
 * Fields that are transient UI state and should never trigger auto-save
 * when changed via SET_FIELD.
 */
const TRANSIENT_FIELDS = new Set<string>([
  'newRepoName',
  'newRepoUrl',
  'newCommandName',
  'newCommandCommand',
  'newQuickLaunchName',
  'newQuickLaunchPrompt',
  'newQuickLaunchCommand',
  'newQuickLaunchTarget',
  'newQuickLaunchMode',
  'newQuickLaunchPersonaId',
  'newDiffName',
  'newDiffCommand',
  'passwordInput',
  'passwordConfirm',
  'selectedCookbookTemplate',
  'saving',
  'loading',
  'error',
  'warning',
  'currentStep',
]);

/**
 * Hook that provides auto-save behavior for the config form.
 *
 * Returns a wrapping dispatch that intercepts actions and triggers
 * debounced saves when appropriate, plus a flushSave() for onBlur
 * and a saveStatus for the UI indicator.
 */
export function useAutoSave(
  state: ConfigFormState,
  rawDispatch: React.Dispatch<ConfigFormAction>,
  saveStatusRef: React.MutableRefObject<SaveStatus>,
  setSaveStatus: (status: SaveStatus) => void
) {
  const { reloadConfig } = useConfig();
  const { error: toastError } = useToast();

  const lastSavedConfigRef = useRef<ConfigFormState | null>(null);
  const debounceTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  // Track the latest state for the debounced save callback
  const stateRef = useRef(state);
  stateRef.current = state;

  const performSave = useCallback(async () => {
    const currentState = stateRef.current;

    // Don't save if still loading or already saving
    if (currentState.loading || saveStatusRef.current === 'saving') return;

    setSaveStatus('saving');
    rawDispatch({ type: 'SET_FIELD', field: 'saving', value: true });

    try {
      const request = buildConfigUpdate(currentState);
      const result = await updateConfig(request);

      reloadConfig();
      localStorage.setItem(CONFIG_UPDATED_KEY, Date.now().toString());

      // Update warnings from server
      rawDispatch({
        type: 'SET_FIELD',
        field: 'authWarnings',
        value: result.warnings || [],
      });
      if (result.warning) {
        rawDispatch({ type: 'SET_FIELD', field: 'warning', value: result.warning });
      }

      // Check if restart needed
      const reloaded = await getConfig();
      rawDispatch({
        type: 'SET_FIELD',
        field: 'apiNeedsRestart',
        value: reloaded.needs_restart || false,
      });

      // Update last saved config
      lastSavedConfigRef.current = { ...stateRef.current };

      setSaveStatus('saved');
    } catch (err) {
      setSaveStatus('error');
      const message = getErrorMessage(err, 'Failed to save config');
      toastError(message);

      // Revert to last saved config
      if (lastSavedConfigRef.current) {
        rawDispatch({ type: 'LOAD_CONFIG', state: lastSavedConfigRef.current });
      }
    } finally {
      rawDispatch({ type: 'SET_FIELD', field: 'saving', value: false });
    }
  }, [rawDispatch, reloadConfig, toastError, setSaveStatus, saveStatusRef]);

  const scheduleSave = useCallback(() => {
    if (debounceTimerRef.current) {
      clearTimeout(debounceTimerRef.current);
    }
    debounceTimerRef.current = setTimeout(() => {
      debounceTimerRef.current = null;
      performSave();
    }, 300);
  }, [performSave]);

  /**
   * Flush any pending debounced save immediately.
   * Call this from text input onBlur handlers.
   */
  const flushSave = useCallback(() => {
    if (debounceTimerRef.current) {
      clearTimeout(debounceTimerRef.current);
      debounceTimerRef.current = null;
      performSave();
    }
  }, [performSave]);

  /**
   * Set the last saved config snapshot. Called after initial config load.
   */
  const setLastSavedConfig = useCallback((config: ConfigFormState) => {
    lastSavedConfigRef.current = { ...config };
  }, []);

  /**
   * Wrapping dispatch that intercepts actions to trigger auto-save.
   */
  const dispatch = useCallback(
    (action: ConfigFormAction) => {
      // Always forward the action to the reducer first
      rawDispatch(action);

      // Check if this action should trigger a save
      if (SAVE_TRIGGER_ACTIONS.has(action.type)) {
        scheduleSave();
        return;
      }

      if (action.type === 'SET_FIELD') {
        const field = action.field as string;
        if (!TRANSIENT_FIELDS.has(field)) {
          scheduleSave();
        }
      }
    },
    [rawDispatch, scheduleSave]
  );

  return {
    dispatch,
    flushSave,
    setLastSavedConfig,
  };
}
