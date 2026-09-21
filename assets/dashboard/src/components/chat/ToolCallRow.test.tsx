import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import ToolCallRow from './ToolCallRow';
import type { ToolSegment } from '../../lib/chat/types';

function tool(overrides: Partial<ToolSegment> = {}): ToolSegment {
  return {
    kind: 'tool',
    id: 'tu-1',
    name: 'Bash',
    input: { command: 'ls -la' },
    inputJson: '{"command":"ls -la"}',
    result: 'file.txt\nfile2.txt',
    state: 'done',
    subtools: [],
    ...overrides,
  };
}

describe('ToolCallRow', () => {
  it('shows the tool name and summary; the result line appears only when expanded', async () => {
    render(<ToolCallRow tool={tool()} />);
    expect(screen.getByText('Bash')).toBeInTheDocument();
    expect(screen.getByText('ls -la')).toBeInTheDocument();
    expect(screen.queryByTestId('chat-tool-result')).not.toBeInTheDocument();
    await userEvent.click(screen.getByTestId('chat-tool-row'));
    expect(screen.getByTestId('chat-tool-result')).toHaveTextContent('file.txt');
  });

  it('summarizes file_path for file tools', () => {
    render(
      <ToolCallRow
        tool={tool({
          name: 'Read',
          input: { file_path: '/tmp/x.go' },
          inputJson: '{"file_path":"/tmp/x.go"}',
        })}
      />
    );
    expect(screen.getByText('/tmp/x.go')).toBeInTheDocument();
  });

  it('status dot reflects the tool state', () => {
    const { rerender } = render(<ToolCallRow tool={tool({ state: 'running' })} />);
    expect(screen.getByTestId('chat-tool-dot').dataset.state).toBe('running');
    rerender(<ToolCallRow tool={tool({ state: 'error' })} />);
    expect(screen.getByTestId('chat-tool-dot').dataset.state).toBe('error');
    rerender(<ToolCallRow tool={tool({ state: 'done' })} />);
    expect(screen.getByTestId('chat-tool-dot').dataset.state).toBe('done');
  });

  it('collapses when clicking inside the expanded details', async () => {
    render(<ToolCallRow tool={tool()} />);
    await userEvent.click(screen.getByTestId('chat-tool-row'));
    expect(screen.getByTestId('chat-tool-details')).toBeInTheDocument();
    await userEvent.click(screen.getByTestId('chat-tool-details'));
    expect(screen.queryByTestId('chat-tool-details')).not.toBeInTheDocument();
  });

  it('does not toggle when the click ends a text selection', async () => {
    render(<ToolCallRow tool={tool()} />);
    await userEvent.click(screen.getByTestId('chat-tool-row'));
    const spy = vi
      .spyOn(window, 'getSelection')
      .mockReturnValue({ isCollapsed: false } as Selection);
    await userEvent.click(screen.getByTestId('chat-tool-details'));
    expect(screen.getByTestId('chat-tool-details')).toBeInTheDocument();
    spy.mockRestore();
  });

  it('expands to the full input and result on click', async () => {
    render(<ToolCallRow tool={tool()} />);
    await userEvent.click(screen.getByTestId('chat-tool-row'));
    const details = screen.getByTestId('chat-tool-details');
    expect(details).toHaveTextContent('file2.txt');
    expect(details).toHaveTextContent(/"command"/);
  });

  it('uses the fallback summary from raw input JSON', () => {
    const longInput = { something: 'x'.repeat(200) };
    render(
      <ToolCallRow
        tool={tool({ name: 'WebFetch', input: longInput, inputJson: JSON.stringify(longInput) })}
      />
    );
    const summary = screen.getByTestId('chat-tool-summary');
    expect(summary.textContent!.length).toBeLessThanOrEqual(80);
  });

  it('shows only the first line of a multi-line command, with an elision marker', () => {
    render(
      <ToolCallRow
        tool={tool({
          input: { command: 'cat a.txt\ncat b.txt\ncat c.txt' },
          inputJson: '{"command":"cat a.txt\\ncat b.txt\\ncat c.txt"}',
        })}
      />
    );
    const summary = screen.getByTestId('chat-tool-summary');
    expect(summary).toHaveTextContent('cat a.txt');
    expect(summary).not.toHaveTextContent('cat b.txt');
    expect(screen.getByTestId('chat-tool-more')).toBeInTheDocument();
  });

  it('shows no elision marker for a single-line command', () => {
    render(<ToolCallRow tool={tool()} />);
    expect(screen.getByTestId('chat-tool-summary')).toHaveTextContent('ls -la');
    expect(screen.queryByTestId('chat-tool-more')).not.toBeInTheDocument();
  });

  it('mutes the row while collapsed and restores full contrast when expanded', async () => {
    render(<ToolCallRow tool={tool()} />);
    const row = screen.getByTestId('chat-tool-row');
    expect(row.className).toContain('toolRowCollapsed');
    await userEvent.click(row);
    expect(row.className).not.toContain('toolRowCollapsed');
  });

  it('hides subtools until expanded, then shows them as one mono line each', async () => {
    render(
      <ToolCallRow
        tool={tool({
          name: 'Agent',
          input: {},
          inputJson: '{}',
          subtools: [
            {
              id: 'sub-1',
              name: 'Bash',
              inputJson: '{"command":"echo sub-hello"}',
              result: 'sub-hello\nsecond',
              state: 'done',
            },
          ],
        })}
      />
    );
    expect(screen.queryByTestId('chat-tool-subtool')).not.toBeInTheDocument();
    await userEvent.click(screen.getByTestId('chat-tool-row'));
    expect(screen.getByTestId('chat-tool-subtool')).toBeInTheDocument();
    expect(screen.getByText('Bash')).toBeInTheDocument();
    expect(screen.getByText('sub-hello')).toBeInTheDocument();
    expect(screen.queryByText('second')).not.toBeInTheDocument();
  });
});
