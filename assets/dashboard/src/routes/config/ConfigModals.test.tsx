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
          quickLaunchEditModal={null}
          tlsModal={null}
          dispatch={dispatch}
          onSaveAuthSecrets={vi.fn()}
          onSaveRunTargetEdit={vi.fn()}
          onSaveQuickLaunchEdit={vi.fn()}
          onSaveTls={vi.fn()}
          onValidateTls={vi.fn()}
          authPublicBaseURL=""
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
          quickLaunchEditModal={null}
          tlsModal={null}
          dispatch={dispatch}
          onSaveAuthSecrets={vi.fn()}
          onSaveRunTargetEdit={vi.fn()}
          onSaveQuickLaunchEdit={vi.fn()}
          onSaveTls={vi.fn()}
          onValidateTls={vi.fn()}
          authPublicBaseURL=""
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
          quickLaunchEditModal={null}
          tlsModal={null}
          dispatch={dispatch}
          onSaveAuthSecrets={onSaveAuthSecrets}
          onSaveRunTargetEdit={vi.fn()}
          onSaveQuickLaunchEdit={vi.fn()}
          onSaveTls={vi.fn()}
          onValidateTls={vi.fn()}
          authPublicBaseURL=""
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
          quickLaunchEditModal={null}
          tlsModal={null}
          dispatch={dispatch}
          onSaveAuthSecrets={vi.fn()}
          onSaveRunTargetEdit={vi.fn()}
          onSaveQuickLaunchEdit={vi.fn()}
          onSaveTls={vi.fn()}
          onValidateTls={vi.fn()}
          authPublicBaseURL=""
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
          quickLaunchEditModal={null}
          tlsModal={null}
          dispatch={dispatch}
          onSaveAuthSecrets={vi.fn()}
          onSaveRunTargetEdit={vi.fn()}
          onSaveQuickLaunchEdit={vi.fn()}
          onSaveTls={vi.fn()}
          onValidateTls={vi.fn()}
          authPublicBaseURL=""
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
            target: { name: 'my-agent', command: 'my-agent --prompt', type: 'promptable' },
            command: 'my-agent --prompt',
            error: '',
          }}
          quickLaunchEditModal={null}
          tlsModal={null}
          dispatch={dispatch}
          onSaveAuthSecrets={vi.fn()}
          onSaveRunTargetEdit={vi.fn()}
          onSaveQuickLaunchEdit={vi.fn()}
          onSaveTls={vi.fn()}
          onValidateTls={vi.fn()}
          authPublicBaseURL=""
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
            target: { name: 'x', command: 'x', type: 'promptable' },
            command: 'x',
            error: '',
          }}
          quickLaunchEditModal={null}
          tlsModal={null}
          dispatch={dispatch}
          onSaveAuthSecrets={vi.fn()}
          onSaveRunTargetEdit={onSaveRunTargetEdit}
          onSaveQuickLaunchEdit={vi.fn()}
          onSaveTls={vi.fn()}
          onValidateTls={vi.fn()}
          authPublicBaseURL=""
        />
      );
      await userEvent.click(screen.getByText('Save'));
      expect(onSaveRunTargetEdit).toHaveBeenCalled();
    });
  });

  describe('quick launch edit modal', () => {
    it('renders prompt textarea for promptable targets', () => {
      render(
        <ConfigModals
          authSecretsModal={null}
          runTargetEditModal={null}
          quickLaunchEditModal={{
            item: { name: 'ql1', target: 'claude', prompt: 'hello' },
            prompt: 'hello',
            isCommandTarget: false,
            error: '',
          }}
          tlsModal={null}
          dispatch={dispatch}
          onSaveAuthSecrets={vi.fn()}
          onSaveRunTargetEdit={vi.fn()}
          onSaveQuickLaunchEdit={vi.fn()}
          onSaveTls={vi.fn()}
          onValidateTls={vi.fn()}
          authPublicBaseURL=""
        />
      );
      expect(screen.getByText('Edit ql1')).toBeInTheDocument();
      expect(screen.getByText('Prompt')).toBeInTheDocument();
      expect(screen.getByDisplayValue('hello')).toBeInTheDocument();
    });

    it('renders command textarea for command targets', () => {
      render(
        <ConfigModals
          authSecretsModal={null}
          runTargetEditModal={null}
          quickLaunchEditModal={{
            item: { name: 'ql2', target: 'build', prompt: null },
            prompt: 'make build',
            isCommandTarget: true,
            error: '',
          }}
          tlsModal={null}
          dispatch={dispatch}
          onSaveAuthSecrets={vi.fn()}
          onSaveRunTargetEdit={vi.fn()}
          onSaveQuickLaunchEdit={vi.fn()}
          onSaveTls={vi.fn()}
          onValidateTls={vi.fn()}
          authPublicBaseURL=""
        />
      );
      expect(screen.getByText('Command')).toBeInTheDocument();
      expect(screen.getByDisplayValue('make build')).toBeInTheDocument();
    });
  });

  describe('tls modal', () => {
    it('renders when tlsModal is set', () => {
      render(
        <ConfigModals
          authSecretsModal={null}
          runTargetEditModal={null}
          quickLaunchEditModal={null}
          tlsModal={{
            certPath: '',
            keyPath: '',
            hostname: '',
            expires: '',
            validating: false,
            error: '',
          }}
          dispatch={dispatch}
          onSaveAuthSecrets={vi.fn()}
          onSaveRunTargetEdit={vi.fn()}
          onSaveQuickLaunchEdit={vi.fn()}
          onSaveTls={vi.fn()}
          onValidateTls={vi.fn()}
          authPublicBaseURL=""
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
          quickLaunchEditModal={null}
          tlsModal={null}
          dispatch={dispatch}
          onSaveAuthSecrets={vi.fn()}
          onSaveRunTargetEdit={vi.fn()}
          onSaveQuickLaunchEdit={vi.fn()}
          onSaveTls={vi.fn()}
          onValidateTls={vi.fn()}
          authPublicBaseURL=""
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
          quickLaunchEditModal={null}
          tlsModal={{
            certPath: '/path/to/cert.pem',
            keyPath: '/path/to/key.pem',
            hostname: '',
            expires: '',
            validating: false,
            error: '',
          }}
          dispatch={dispatch}
          onSaveAuthSecrets={vi.fn()}
          onSaveRunTargetEdit={vi.fn()}
          onSaveQuickLaunchEdit={vi.fn()}
          onSaveTls={vi.fn()}
          onValidateTls={onValidateTls}
          authPublicBaseURL=""
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
          quickLaunchEditModal={null}
          tlsModal={{
            certPath: '/path/to/cert.pem',
            keyPath: '/path/to/key.pem',
            hostname: 'schmux.local',
            expires: '2027-01-01T00:00:00Z',
            validating: false,
            error: '',
          }}
          dispatch={dispatch}
          onSaveAuthSecrets={vi.fn()}
          onSaveRunTargetEdit={vi.fn()}
          onSaveQuickLaunchEdit={vi.fn()}
          onSaveTls={vi.fn()}
          onValidateTls={vi.fn()}
          authPublicBaseURL=""
        />
      );
      expect(screen.getByText('Valid certificate')).toBeInTheDocument();
      expect(screen.getByText('schmux.local')).toBeInTheDocument();
    });
  });
});
