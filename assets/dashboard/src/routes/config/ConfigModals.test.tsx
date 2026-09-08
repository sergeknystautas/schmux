import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import ConfigModals from './ConfigModals';
import type { ConfigFormAction } from './useConfigForm';

const dispatch = vi.fn<(action: ConfigFormAction) => void>();

const baseProps = {
  authSecretsModal: null,
  authEnabled: true,
  runTargetEditModal: null,
  quickLaunchDialogModal: null,
  tlsModal: null,
  pastebinEditModal: null,
  dispatch,
  onSaveAuthSecrets: vi.fn(),
  onSaveRunTargetEdit: vi.fn(),
  onSaveQuickLaunchDialog: vi.fn(),
  onSavePastebinEdit: vi.fn(),
  onSaveTls: vi.fn(),
  onValidateTls: vi.fn(),
  authPublicBaseURL: '',
  models: [] as any[],
  personas: [] as any[],
  fenceAvailable: false,
  chatSessions: false,
};

describe('ConfigModals', () => {
  describe('auth secrets modal', () => {
    it('renders when authSecretsModal is set', () => {
      render(
        <ConfigModals
          {...baseProps}
          authSecretsModal={{
            clientId: '',
            clientSecret: '',
            clientSecretWasSet: false,
            error: '',
          }}
        />
      );
      expect(screen.getByText('GitHub OAuth Credentials')).toBeInTheDocument();
      expect(screen.getByPlaceholderText('Ov23li...')).toBeInTheDocument();
      expect(screen.getByPlaceholderText('Enter client secret')).toBeInTheDocument();
    });

    it('does not render when authSecretsModal is null', () => {
      render(<ConfigModals {...baseProps} />);
      expect(screen.queryByText('GitHub OAuth Credentials')).not.toBeInTheDocument();
    });

    it('calls onSaveAuthSecrets when Save is clicked', async () => {
      const onSaveAuthSecrets = vi.fn();
      render(
        <ConfigModals
          {...baseProps}
          authSecretsModal={{
            clientId: 'id',
            clientSecret: 'secret',
            clientSecretWasSet: false,
            error: '',
          }}
          onSaveAuthSecrets={onSaveAuthSecrets}
        />
      );
      await userEvent.click(screen.getByText('Save'));
      expect(onSaveAuthSecrets).toHaveBeenCalled();
    });

    it('shows error when set', () => {
      render(
        <ConfigModals
          {...baseProps}
          authSecretsModal={{
            clientId: '',
            clientSecret: '',
            clientSecretWasSet: false,
            error: 'Bad creds',
          }}
        />
      );
      expect(screen.getByText('Bad creds')).toBeInTheDocument();
    });

    it('dispatches close on Cancel click', async () => {
      dispatch.mockClear();
      render(
        <ConfigModals
          {...baseProps}
          authSecretsModal={{
            clientId: '',
            clientSecret: '',
            clientSecretWasSet: false,
            error: '',
          }}
        />
      );
      await userEvent.click(screen.getByText('Cancel'));
      expect(dispatch).toHaveBeenCalledWith({ type: 'SET_AUTH_SECRETS_MODAL', modal: null });
    });

    it('labels the primary button "Save & enable" when auth is off', () => {
      render(
        <ConfigModals
          {...baseProps}
          authSecretsModal={{
            clientId: '',
            clientSecret: '',
            clientSecretWasSet: false,
            error: '',
          }}
          authEnabled={false}
        />
      );
      expect(screen.getByRole('button', { name: /save & enable/i })).toBeInTheDocument();
    });

    it('labels the primary button "Save" when auth is already on', () => {
      render(
        <ConfigModals
          {...baseProps}
          authSecretsModal={{
            clientId: 'Ov23li',
            clientSecret: '',
            clientSecretWasSet: true,
            error: '',
          }}
        />
      );
      expect(screen.getByRole('button', { name: /^save$/i })).toBeInTheDocument();
    });
  });

  describe('run target edit modal', () => {
    it('renders with target name and command textarea', () => {
      render(
        <ConfigModals
          {...baseProps}
          runTargetEditModal={{
            target: { name: 'my-agent', command: 'my-agent --prompt' },
            command: 'my-agent --prompt',
            error: '',
          }}
        />
      );
      expect(screen.getByText('Edit my-agent')).toBeInTheDocument();
      expect(screen.getByDisplayValue('my-agent --prompt')).toBeInTheDocument();
    });

    it('calls onSaveRunTargetEdit when Save is clicked', async () => {
      const onSaveRunTargetEdit = vi.fn();
      render(
        <ConfigModals
          {...baseProps}
          runTargetEditModal={{
            target: { name: 'x', command: 'x' },
            command: 'x',
            error: '',
          }}
          onSaveRunTargetEdit={onSaveRunTargetEdit}
        />
      );
      await userEvent.click(screen.getByText('Save'));
      expect(onSaveRunTargetEdit).toHaveBeenCalled();
    });
  });

  describe('quick launch dialog modal', () => {
    const mockModels = [
      { id: 'claude-sonnet', display_name: 'Claude Sonnet', configured: true },
    ] as any;

    const agentModal = {
      mode: 'add' as const,
      kind: 'agent' as const,
      name: '',
      target: '',
      prompt: '',
      error: '',
    };

    const commandModal = {
      mode: 'add' as const,
      kind: 'command' as const,
      name: '',
      command: '',
      error: '',
    };

    const editAgentModal = {
      mode: 'edit' as const,
      kind: 'agent' as const,
      name: 'code-review',
      originalName: 'code-review',
      target: 'claude-sonnet',
      prompt: 'Review this code',
      error: '',
    };

    it('renders agent dialog with model select and prompt textarea', () => {
      render(
        <ConfigModals {...baseProps} models={mockModels} quickLaunchDialogModal={agentModal} />
      );
      expect(screen.getByText('Add Quick Launch')).toBeInTheDocument();
      expect(screen.getByText('Model')).toBeInTheDocument();
      expect(screen.getByText('Prompt')).toBeInTheDocument();
      expect(screen.queryByText('Command')).not.toBeInTheDocument();
    });

    it('renders command dialog with command textarea', () => {
      render(
        <ConfigModals {...baseProps} models={mockModels} quickLaunchDialogModal={commandModal} />
      );
      expect(screen.getByText('Add Quick Launch')).toBeInTheDocument();
      expect(screen.getByText('Command')).toBeInTheDocument();
      expect(screen.queryByText('Model')).not.toBeInTheDocument();
    });

    it('renders edit mode with title and read-only name', () => {
      render(
        <ConfigModals {...baseProps} models={mockModels} quickLaunchDialogModal={editAgentModal} />
      );
      expect(screen.getByText('Edit code-review')).toBeInTheDocument();
      const nameInput = screen.getByDisplayValue('code-review') as HTMLInputElement;
      expect(nameInput.readOnly).toBe(true);
    });

    it('calls save handler on Save click', () => {
      const onSave = vi.fn();
      render(
        <ConfigModals
          {...baseProps}
          models={mockModels}
          quickLaunchDialogModal={agentModal}
          onSaveQuickLaunchDialog={onSave}
        />
      );
      const saveBtn = screen.getAllByText('Save').find((el) => el.closest('.modal__footer'));
      saveBtn?.click();
      expect(onSave).toHaveBeenCalled();
    });

    it('dispatches close on Cancel click', () => {
      const localDispatch = vi.fn();
      render(
        <ConfigModals
          {...baseProps}
          models={mockModels}
          quickLaunchDialogModal={agentModal}
          dispatch={localDispatch}
        />
      );
      const cancelBtn = screen.getAllByText('Cancel').find((el) => el.closest('.modal__footer'));
      cancelBtn?.click();
      expect(localDispatch).toHaveBeenCalledWith({
        type: 'SET_QUICK_LAUNCH_DIALOG_MODAL',
        modal: null,
      });
    });

    it('shows stale model as unavailable option in edit mode', () => {
      const staleModal = {
        mode: 'edit' as const,
        kind: 'agent' as const,
        name: 'old-preset',
        originalName: 'old-preset',
        target: 'deleted-model',
        prompt: 'Do stuff',
        error: '',
      };
      render(
        <ConfigModals {...baseProps} models={mockModels} quickLaunchDialogModal={staleModal} />
      );
      expect(screen.getByText('deleted-model (unavailable)')).toBeInTheDocument();
    });

    it('displays error message', () => {
      const errorModal = { ...agentModal, error: 'Name is required' };
      render(
        <ConfigModals {...baseProps} models={mockModels} quickLaunchDialogModal={errorModal} />
      );
      expect(screen.getByText('Name is required')).toBeInTheDocument();
    });

    // The fence checkbox is an experimental opt-in: it appears only when the
    // fence binary is present and fence_mode is not "disabled".
    it('hides the fence checkbox when fence is unavailable', () => {
      render(
        <ConfigModals
          {...baseProps}
          models={mockModels}
          fenceAvailable={false}
          quickLaunchDialogModal={commandModal}
        />
      );
      expect(screen.queryByTestId('quick-launch-fence')).toBeNull();
    });

    it('shows the fence checkbox on the command form when fence is available', () => {
      render(
        <ConfigModals
          {...baseProps}
          models={mockModels}
          fenceAvailable={true}
          quickLaunchDialogModal={commandModal}
        />
      );
      expect(screen.getByTestId('quick-launch-fence')).toBeTruthy();
    });

    it('hides the chat checkbox when chat sessions are disabled', () => {
      render(
        <ConfigModals
          {...baseProps}
          chatSessions={false}
          quickLaunchDialogModal={{
            mode: 'add',
            kind: 'agent',
            name: 'review',
            target: 'claude',
            prompt: 'review it',
            error: '',
          }}
        />
      );
      expect(screen.queryByTestId('quick-launch-chat')).toBeNull();
    });

    // Add Agent starts with no target chosen; the checkbox must still be
    // there. Whether the eventual target has a chat mode is the spawn
    // gate's call, not the dialog's.
    it('shows the chat checkbox on the agent form before a target is picked', () => {
      render(
        <ConfigModals
          {...baseProps}
          chatSessions={true}
          quickLaunchDialogModal={{
            mode: 'add',
            kind: 'agent',
            name: '',
            target: '',
            prompt: '',
            error: '',
          }}
        />
      );
      expect(screen.getByTestId('quick-launch-chat')).toBeTruthy();
    });

    // Chat is agent-only: a chat session requires a target.
    it('hides the chat checkbox on the command form', () => {
      render(
        <ConfigModals
          {...baseProps}
          chatSessions={true}
          quickLaunchDialogModal={{
            mode: 'add',
            kind: 'command',
            name: 'build',
            command: 'make build',
            error: '',
          }}
        />
      );
      expect(screen.queryByTestId('quick-launch-chat')).toBeNull();
    });

    // A stale value must stay visible so it can be cleared without editing JSON.
    it('shows a disabled-feature checkbox when the preset already has the value set', () => {
      render(
        <ConfigModals
          {...baseProps}
          models={mockModels}
          quickLaunchDialogModal={{
            mode: 'edit',
            kind: 'command',
            name: 'build',
            originalName: 'build',
            command: 'make build',
            fence: true,
            error: '',
          }}
        />
      );
      expect(screen.getByTestId('quick-launch-fence')).toBeTruthy();
    });
  });

  describe('tls modal', () => {
    it('renders when tlsModal is set', () => {
      render(
        <ConfigModals
          {...baseProps}
          tlsModal={{
            certPath: '',
            keyPath: '',
            hostname: '',
            expires: '',
            validating: false,
            error: '',
          }}
        />
      );
      expect(screen.getByText('TLS Certificate')).toBeInTheDocument();
      expect(screen.getByPlaceholderText('~/.schmux/tls/schmux.local.pem')).toBeInTheDocument();
    });

    it('does not render when tlsModal is null', () => {
      render(<ConfigModals {...baseProps} />);
      expect(screen.queryByText('TLS Certificate')).not.toBeInTheDocument();
    });

    it('calls onValidateTls when Validate is clicked', async () => {
      const onValidateTls = vi.fn();
      render(
        <ConfigModals
          {...baseProps}
          tlsModal={{
            certPath: '/path/to/cert.pem',
            keyPath: '/path/to/key.pem',
            hostname: '',
            expires: '',
            validating: false,
            error: '',
          }}
          onValidateTls={onValidateTls}
        />
      );
      await userEvent.click(screen.getByText('Validate'));
      expect(onValidateTls).toHaveBeenCalled();
    });

    it('shows success banner when hostname is set', () => {
      render(
        <ConfigModals
          {...baseProps}
          tlsModal={{
            certPath: '/path/to/cert.pem',
            keyPath: '/path/to/key.pem',
            hostname: 'schmux.local',
            expires: '2027-01-01T00:00:00Z',
            validating: false,
            error: '',
          }}
        />
      );
      expect(screen.getByText('Valid certificate')).toBeInTheDocument();
      expect(screen.getByText('schmux.local')).toBeInTheDocument();
    });
  });
});
