import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import FlightForm from './FlightForm';

const repos = [
  { url: 'git@example.com:a.git', name: 'alpha' },
  { url: 'git@example.com:b.git', name: 'beta' },
];
const targets = [
  { id: 'claude', label: 'Claude' },
  { id: 'codex', label: 'Codex' },
];

function renderForm(props: Partial<Parameters<typeof FlightForm>[0]> = {}) {
  const onSubmit = vi.fn().mockResolvedValue(undefined);
  render(
    <FlightForm
      repos={repos}
      targets={targets}
      draft={{
        repo: repos[0].url,
        prompt: '',
        short_name: '',
        planner_model: 'claude',
        executor_model: 'claude',
        executorSelected: false,
      }}
      submitLabel="Create flight"
      onSubmit={onSubmit}
      onCancel={vi.fn()}
      {...props}
    />
  );
  return onSubmit;
}

describe('FlightForm', () => {
  it('requires a model choice when there is no remembered selection', async () => {
    const onSubmit = renderForm({ draft: undefined });
    await userEvent.type(screen.getByLabelText('Prompt'), 'Do it');
    expect(screen.getByLabelText('Planner')).toHaveValue('');
    expect(screen.getByLabelText('Executor')).toHaveValue('');
    expect(screen.getByTestId('flight-form-submit')).toBeDisabled();
    await userEvent.selectOptions(screen.getByLabelText('Planner'), 'codex');
    await userEvent.click(screen.getByTestId('flight-form-submit'));
    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({ planner_model: 'codex', executor_model: 'codex' })
    );
  });
  it('starts with the focused spawn prompt before the compact selectors', () => {
    renderForm();
    const prompt = screen.getByLabelText('Prompt');
    expect(prompt).toHaveFocus();
    expect(screen.queryByLabelText('Stage')).not.toBeInTheDocument();
    expect(
      prompt.compareDocumentPosition(screen.getByLabelText('Planner')) &
        Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy();
  });

  it.each(['Meta', 'Control'])('submits with %s+Enter from a selector', async (modifier) => {
    const onSubmit = renderForm();
    await userEvent.type(screen.getByLabelText('Prompt'), 'Fix login');
    screen.getByLabelText('Repository').focus();
    await userEvent.keyboard(`{${modifier}>}{Enter}{/${modifier}}`);
    expect(onSubmit).toHaveBeenCalledTimes(1);
    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({
        prompt: 'Fix login',
        planner_model: 'claude',
        executor_model: 'claude',
      })
    );
  });

  it('follows the planner until the executor is explicitly chosen', async () => {
    renderForm();
    await userEvent.selectOptions(screen.getByLabelText('Planner'), 'codex');
    expect(screen.getByLabelText('Executor')).toHaveValue('codex');
    await userEvent.selectOptions(screen.getByLabelText('Executor'), 'claude');
    await userEvent.selectOptions(screen.getByLabelText('Planner'), 'claude');
    await userEvent.selectOptions(screen.getByLabelText('Planner'), 'codex');
    expect(screen.getByLabelText('Executor')).toHaveValue('claude');
  });

  it('requires a prompt before it can submit', async () => {
    renderForm();
    expect(screen.getByTestId('flight-form-submit')).toBeDisabled();

    await userEvent.type(screen.getByLabelText('Prompt'), '   ');
    expect(screen.getByTestId('flight-form-submit')).toBeDisabled();
  });

  it('submits the chosen repo, targets, trimmed short name, and the prompt verbatim', async () => {
    const onSubmit = renderForm();

    await userEvent.selectOptions(screen.getByLabelText('Repository'), 'git@example.com:b.git');
    await userEvent.type(screen.getByLabelText('Short name'), '  Login fix  ');
    await userEvent.selectOptions(screen.getByLabelText('Executor'), 'codex');
    await userEvent.type(screen.getByLabelText('Prompt'), '  Fix login.{enter}Keep tests.  ');
    await userEvent.click(screen.getByTestId('flight-form-submit'));

    expect(onSubmit).toHaveBeenCalledWith({
      repo: 'git@example.com:b.git',
      prompt: '  Fix login.\nKeep tests.  ',
      short_name: 'Login fix',
      planner_model: 'claude',
      executor_model: 'codex',
    });
  });

  it('lets the planner and executor be the same target', async () => {
    const onSubmit = renderForm({ targets: [targets[0]] });

    await userEvent.type(screen.getByLabelText('Prompt'), 'Do it');
    await userEvent.click(screen.getByTestId('flight-form-submit'));

    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({ planner_model: 'claude', executor_model: 'claude' })
    );
  });

  it('shows the backend validation error and keeps the input', async () => {
    renderForm({
      onSubmit: vi
        .fn()
        .mockRejectedValue(new Error('invalid flight request: planner_model: no chat mode')),
    });

    await userEvent.type(screen.getByLabelText('Prompt'), 'Do it');
    await userEvent.click(screen.getByTestId('flight-form-submit'));

    expect(screen.getByTestId('flight-form-error')).toHaveTextContent(
      'invalid flight request: planner_model: no chat mode'
    );
    expect(screen.getByLabelText('Prompt')).toHaveValue('Do it');
  });

  it('explains what is missing instead of rendering an unusable form', () => {
    renderForm({ repos: [] });
    expect(screen.getByTestId('flight-form-no-repos')).toBeInTheDocument();
  });

  it('explains that a chat-capable target is required', () => {
    renderForm({ targets: [] });
    expect(screen.getByTestId('flight-form-no-targets')).toBeInTheDocument();
    expect(screen.queryByTestId('flight-form')).not.toBeInTheDocument();
  });
});
