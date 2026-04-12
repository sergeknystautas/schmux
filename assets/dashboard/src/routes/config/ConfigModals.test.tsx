import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import ConfigModals from './ConfigModals';
import type { ConfigFormAction } from './useConfigForm';

const dispatch = vi.fn<(action: ConfigFormAction) => void>();

describe('ConfigModals', () => {
  describe('auth secrets modal', () => {
    it('renders when authSecretsModal is set', () => {
      render(
        <ConfigModals
          authSecretsModal={{
            clientId: '',
            clientSecret: '',
            clientSecretWasSet: false,
            error: '',
          }}
          runTargetEditModal={null}
          quickLaunchDialogModal={null}
          tlsModal={null}
          pastebinEditModal={null}
          dispatch={dispatch}
          onSaveAuthSecrets={vi.fn()}
          onSaveRunTargetEdit={vi.fn()}
          onSaveQuickLaunchDialog={vi.fn()}
          onSavePastebinEdit={vi.fn()}
          onSaveTls={vi.fn()}
          onValidateTls={vi.fn()}
          authPublicBaseURL=""
          models={[]}
          personas={[]}
        />
      );
      expect(screen.getByText('GitHub OAuth Credentials')).toBeInTheDocument();
      expect(screen.getByPlaceholderText('Ov23li...')).toBeInTheDocument();
      expect(screen.getByPlaceholderText('Enter client secret')).toBeInTheDocument();
    });

    it('does not render when authSecretsModal is null', () => {
      render(
        <ConfigModals
          authSecretsModal={null}
          runTargetEditModal={null}
          quickLaunchDialogModal={null}
          tlsModal={null}
          pastebinEditModal={null}
          dispatch={dispatch}
          onSaveAuthSecrets={vi.fn()}
          onSaveRunTargetEdit={vi.fn()}
          onSaveQuickLaunchDialog={vi.fn()}
          onSavePastebinEdit={vi.fn()}
          onSaveTls={vi.fn()}
          onValidateTls={vi.fn()}
          authPublicBaseURL=""
          models={[]}
          personas={[]}
        />
      );
      expect(screen.queryByText('GitHub OAuth Credentials')).not.toBeInTheDocument();
    });

    it('calls onSaveAuthSecrets when Save is clicked', async () => {
      const onSaveAuthSecrets = vi.fn();
      render(
        <ConfigModals
          authSecretsModal={{
            clientId: 'id',
            clientSecret: 'secret',
            clientSecretWasSet: false,
            error: '',
          }}
          runTargetEditModal={null}
          quickLaunchDialogModal={null}
          tlsModal={null}
          pastebinEditModal={null}
          dispatch={dispatch}
          onSaveAuthSecrets={onSaveAuthSecrets}
          onSaveRunTargetEdit={vi.fn()}
          onSaveQuickLaunchDialog={vi.fn()}
          onSavePastebinEdit={vi.fn()}
          onSaveTls={vi.fn()}
          onValidateTls={vi.fn()}
          authPublicBaseURL=""
          models={[]}
          personas={[]}
        />
      );
      await userEvent.click(screen.getByText('Save'));
      expect(onSaveAuthSecrets).toHaveBeenCalled();
    });

    it('shows error when set', () => {
      render(
        <ConfigModals
          authSecretsModal={{
            clientId: '',
            clientSecret: '',
            clientSecretWasSet: false,
            error: 'Bad creds',
          }}
          runTargetEditModal={null}
          quickLaunchDialogModal={null}
          tlsModal={null}
          pastebinEditModal={null}
          dispatch={dispatch}
          onSaveAuthSecrets={vi.fn()}
          onSaveRunTargetEdit={vi.fn()}
          onSaveQuickLaunchDialog={vi.fn()}
          onSavePastebinEdit={vi.fn()}
          onSaveTls={vi.fn()}
          onValidateTls={vi.fn()}
          authPublicBaseURL=""
          models={[]}
          personas={[]}
        />
      );
      expect(screen.getByText('Bad creds')).toBeInTheDocument();
    });

    it('dispatches close on Cancel click', async () => {
      dispatch.mockClear();
      render(
        <ConfigModals
          authSecretsModal={{
            clientId: '',
            clientSecret: '',
            clientSecretWasSet: false,
            error: '',
          }}
          runTargetEditModal={null}
          quickLaunchDialogModal={null}
          tlsModal={null}
          pastebinEditModal={null}
          dispatch={dispatch}
          onSaveAuthSecrets={vi.fn()}
          onSaveRunTargetEdit={vi.fn()}
          onSaveQuickLaunchDialog={vi.fn()}
          onSavePastebinEdit={vi.fn()}
          onSaveTls={vi.fn()}
          onValidateTls={vi.fn()}
          authPublicBaseURL=""
          models={[]}
          personas={[]}
        />
      );
      await userEvent.click(screen.getByText('Cancel'));
      expect(dispatch).toHaveBeenCalledWith({ type: 'SET_AUTH_SECRETS_MODAL', modal: null });
    });
  });

  describe('run target edit modal', () => {
    it('renders with target name and command textarea', () => {
      render(
        <ConfigModals
          authSecretsModal={null}
          runTargetEditModal={{
            target: { name: 'my-agent', command: 'my-agent --prompt' },
            command: 'my-agent --prompt',
            error: '',
          }}
          quickLaunchDialogModal={null}
          tlsModal={null}
          pastebinEditModal={null}
          dispatch={dispatch}
          onSaveAuthSecrets={vi.fn()}
          onSaveRunTargetEdit={vi.fn()}
          onSaveQuickLaunchDialog={vi.fn()}
          onSavePastebinEdit={vi.fn()}
          onSaveTls={vi.fn()}
          onValidateTls={vi.fn()}
          authPublicBaseURL=""
          models={[]}
          personas={[]}
        />
      );
      expect(screen.getByText('Edit my-agent')).toBeInTheDocument();
      expect(screen.getByDisplayValue('my-agent --prompt')).toBeInTheDocument();
    });

    it('calls onSaveRunTargetEdit when Save is clicked', async () => {
      const onSaveRunTargetEdit = vi.fn();
      render(
        <ConfigModals
          authSecretsModal={null}
          runTargetEditModal={{
            target: { name: 'x', command: 'x' },
            command: 'x',
            error: '',
          }}
          quickLaunchDialogModal={null}
          tlsModal={null}
          pastebinEditModal={null}
          dispatch={dispatch}
          onSaveAuthSecrets={vi.fn()}
          onSaveRunTargetEdit={onSaveRunTargetEdit}
          onSaveQuickLaunchDialog={vi.fn()}
          onSavePastebinEdit={vi.fn()}
          onSaveTls={vi.fn()}
          onValidateTls={vi.fn()}
          authPublicBaseURL=""
          models={[]}
          personas={[]}
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
        <ConfigModals
          authSecretsModal={null}
          runTargetEditModal={null}
          quickLaunchDialogModal={agentModal}
          tlsModal={null}
          pastebinEditModal={null}
          dispatch={dispatch}
          onSaveAuthSecrets={vi.fn()}
          onSaveRunTargetEdit={vi.fn()}
          onSaveQuickLaunchDialog={vi.fn()}
          onSavePastebinEdit={vi.fn()}
          onSaveTls={vi.fn()}
          onValidateTls={vi.fn()}
          authPublicBaseURL=""
          models={mockModels}
          personas={[]}
        />
      );
      expect(screen.getByText('Add Quick Launch')).toBeInTheDocument();
      expect(screen.getByText('Model')).toBeInTheDocument();
      expect(screen.getByText('Prompt')).toBeInTheDocument();
      expect(screen.queryByText('Command')).not.toBeInTheDocument();
    });

    it('renders command dialog with command textarea', () => {
      render(
        <ConfigModals
          authSecretsModal={null}
          runTargetEditModal={null}
          quickLaunchDialogModal={commandModal}
          tlsModal={null}
          pastebinEditModal={null}
          dispatch={dispatch}
          onSaveAuthSecrets={vi.fn()}
          onSaveRunTargetEdit={vi.fn()}
          onSaveQuickLaunchDialog={vi.fn()}
          onSavePastebinEdit={vi.fn()}
          onSaveTls={vi.fn()}
          onValidateTls={vi.fn()}
          authPublicBaseURL=""
          models={mockModels}
          personas={[]}
        />
      );
      expect(screen.getByText('Add Quick Launch')).toBeInTheDocument();
      expect(screen.getByText('Command')).toBeInTheDocument();
      expect(screen.queryByText('Model')).not.toBeInTheDocument();
    });

    it('renders edit mode with title and read-only name', () => {
      render(
        <ConfigModals
          authSecretsModal={null}
          runTargetEditModal={null}
          quickLaunchDialogModal={editAgentModal}
          tlsModal={null}
          pastebinEditModal={null}
          dispatch={dispatch}
          onSaveAuthSecrets={vi.fn()}
          onSaveRunTargetEdit={vi.fn()}
          onSaveQuickLaunchDialog={vi.fn()}
          onSavePastebinEdit={vi.fn()}
          onSaveTls={vi.fn()}
          onValidateTls={vi.fn()}
          authPublicBaseURL=""
          models={mockModels}
          personas={[]}
        />
      );
      expect(screen.getByText('Edit code-review')).toBeInTheDocument();
      const nameInput = screen.getByDisplayValue('code-review') as HTMLInputElement;
      expect(nameInput.readOnly).toBe(true);
    });

    it('calls save handler on Save click', () => {
      const onSave = vi.fn();
      render(
        <ConfigModals
          authSecretsModal={null}
          runTargetEditModal={null}
          quickLaunchDialogModal={agentModal}
          tlsModal={null}
          pastebinEditModal={null}
          dispatch={dispatch}
          onSaveAuthSecrets={vi.fn()}
          onSaveRunTargetEdit={vi.fn()}
          onSaveQuickLaunchDialog={onSave}
          onSavePastebinEdit={vi.fn()}
          onSaveTls={vi.fn()}
          onValidateTls={vi.fn()}
          authPublicBaseURL=""
          models={mockModels}
          personas={[]}
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
          authSecretsModal={null}
          runTargetEditModal={null}
          quickLaunchDialogModal={agentModal}
          tlsModal={null}
          pastebinEditModal={null}
          dispatch={localDispatch}
          onSaveAuthSecrets={vi.fn()}
          onSaveRunTargetEdit={vi.fn()}
          onSaveQuickLaunchDialog={vi.fn()}
          onSavePastebinEdit={vi.fn()}
          onSaveTls={vi.fn()}
          onValidateTls={vi.fn()}
          authPublicBaseURL=""
          models={mockModels}
          personas={[]}
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
        <ConfigModals
          authSecretsModal={null}
          runTargetEditModal={null}
          quickLaunchDialogModal={staleModal}
          tlsModal={null}
          pastebinEditModal={null}
          dispatch={dispatch}
          onSaveAuthSecrets={vi.fn()}
          onSaveRunTargetEdit={vi.fn()}
          onSaveQuickLaunchDialog={vi.fn()}
          onSavePastebinEdit={vi.fn()}
          onSaveTls={vi.fn()}
          onValidateTls={vi.fn()}
          authPublicBaseURL=""
          models={mockModels}
          personas={[]}
        />
      );
      expect(screen.getByText('deleted-model (unavailable)')).toBeInTheDocument();
    });

    it('displays error message', () => {
      const errorModal = { ...agentModal, error: 'Name is required' };
      render(
        <ConfigModals
          authSecretsModal={null}
          runTargetEditModal={null}
          quickLaunchDialogModal={errorModal}
          tlsModal={null}
          pastebinEditModal={null}
          dispatch={dispatch}
          onSaveAuthSecrets={vi.fn()}
          onSaveRunTargetEdit={vi.fn()}
          onSaveQuickLaunchDialog={vi.fn()}
          onSavePastebinEdit={vi.fn()}
          onSaveTls={vi.fn()}
          onValidateTls={vi.fn()}
          authPublicBaseURL=""
          models={mockModels}
          personas={[]}
        />
      );
      expect(screen.getByText('Name is required')).toBeInTheDocument();
    });
  });

  describe('tls modal', () => {
    it('renders when tlsModal is set', () => {
      render(
        <ConfigModals
          authSecretsModal={null}
          runTargetEditModal={null}
          quickLaunchDialogModal={null}
          tlsModal={{
            certPath: '',
            keyPath: '',
            hostname: '',
            expires: '',
            validating: false,
            error: '',
          }}
          pastebinEditModal={null}
          dispatch={dispatch}
          onSaveAuthSecrets={vi.fn()}
          onSaveRunTargetEdit={vi.fn()}
          onSaveQuickLaunchDialog={vi.fn()}
          onSavePastebinEdit={vi.fn()}
          onSaveTls={vi.fn()}
          onValidateTls={vi.fn()}
          authPublicBaseURL=""
          models={[]}
          personas={[]}
        />
      );
      expect(screen.getByText('TLS Certificate')).toBeInTheDocument();
      expect(screen.getByPlaceholderText('~/.schmux/tls/schmux.local.pem')).toBeInTheDocument();
    });

    it('does not render when tlsModal is null', () => {
      render(
        <ConfigModals
          authSecretsModal={null}
          runTargetEditModal={null}
          quickLaunchDialogModal={null}
          tlsModal={null}
          pastebinEditModal={null}
          dispatch={dispatch}
          onSaveAuthSecrets={vi.fn()}
          onSaveRunTargetEdit={vi.fn()}
          onSaveQuickLaunchDialog={vi.fn()}
          onSavePastebinEdit={vi.fn()}
          onSaveTls={vi.fn()}
          onValidateTls={vi.fn()}
          authPublicBaseURL=""
          models={[]}
          personas={[]}
        />
      );
      expect(screen.queryByText('TLS Certificate')).not.toBeInTheDocument();
    });

    it('calls onValidateTls when Validate is clicked', async () => {
      const onValidateTls = vi.fn();
      render(
        <ConfigModals
          authSecretsModal={null}
          runTargetEditModal={null}
          quickLaunchDialogModal={null}
          tlsModal={{
            certPath: '/path/to/cert.pem',
            keyPath: '/path/to/key.pem',
            hostname: '',
            expires: '',
            validating: false,
            error: '',
          }}
          pastebinEditModal={null}
          dispatch={dispatch}
          onSaveAuthSecrets={vi.fn()}
          onSaveRunTargetEdit={vi.fn()}
          onSaveQuickLaunchDialog={vi.fn()}
          onSavePastebinEdit={vi.fn()}
          onSaveTls={vi.fn()}
          onValidateTls={onValidateTls}
          authPublicBaseURL=""
          models={[]}
          personas={[]}
        />
      );
      await userEvent.click(screen.getByText('Validate'));
      expect(onValidateTls).toHaveBeenCalled();
    });

    it('shows success banner when hostname is set', () => {
      render(
        <ConfigModals
          authSecretsModal={null}
          runTargetEditModal={null}
          quickLaunchDialogModal={null}
          tlsModal={{
            certPath: '/path/to/cert.pem',
            keyPath: '/path/to/key.pem',
            hostname: 'schmux.local',
            expires: '2027-01-01T00:00:00Z',
            validating: false,
            error: '',
          }}
          pastebinEditModal={null}
          dispatch={dispatch}
          onSaveAuthSecrets={vi.fn()}
          onSaveRunTargetEdit={vi.fn()}
          onSaveQuickLaunchDialog={vi.fn()}
          onSavePastebinEdit={vi.fn()}
          onSaveTls={vi.fn()}
          onValidateTls={vi.fn()}
          authPublicBaseURL=""
          models={[]}
          personas={[]}
        />
      );
      expect(screen.getByText('Valid certificate')).toBeInTheDocument();
      expect(screen.getByText('schmux.local')).toBeInTheDocument();
    });
  });
});
