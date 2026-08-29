import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import PromptTextarea from './PromptTextarea';

function renderTextarea(props: Partial<React.ComponentProps<typeof PromptTextarea>> = {}) {
  return render(
    <PromptTextarea
      value={props.value ?? ''}
      onChange={props.onChange ?? vi.fn()}
      commands={props.commands ?? ['/resume']}
      onSelectCommand={props.onSelectCommand ?? vi.fn()}
      data-testid="prompt"
      {...props}
    />
  );
}

describe('PromptTextarea disabled', () => {
  it('disables the textarea when disabled is set', () => {
    renderTextarea({ disabled: true });
    expect(screen.getByTestId('prompt')).toBeDisabled();
  });

  it('stays enabled by default', () => {
    renderTextarea();
    expect(screen.getByTestId('prompt')).not.toBeDisabled();
  });

  it('does not render the slash menu while disabled, even with a slash value', () => {
    // Type "/" while enabled to activate the menu, then re-render disabled.
    const { rerender } = renderTextarea({ value: '/' });
    const textarea = screen.getByTestId('prompt');
    fireEvent.change(textarea, { target: { value: '/', selectionStart: 1 } });

    rerender(
      <PromptTextarea
        value="/"
        onChange={vi.fn()}
        commands={['/resume']}
        onSelectCommand={vi.fn()}
        data-testid="prompt"
        disabled
      />
    );
    expect(screen.queryByText('/resume')).not.toBeInTheDocument();
  });
});
