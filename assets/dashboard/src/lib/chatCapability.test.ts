import { describe, it, expect } from 'vitest';
import { targetSupportsChat } from './chatCapability';
import type { Model, RunnerInfo } from './types';

const model = (id: string, runners: string[]) => ({ id, runners }) as unknown as Model;

const runners: Record<string, RunnerInfo> = {
  claude: { available: true, capabilities: ['interactive', 'chat'] },
  codex: { available: true, capabilities: ['interactive', 'chat'] },
  gemini: { available: true, capabilities: ['interactive'] },
  offline: { available: false, capabilities: ['chat'] },
};

describe('targetSupportsChat', () => {
  it('is true for a model whose first available runner declares chat', () => {
    expect(targetSupportsChat('sonnet', [model('sonnet', ['offline', 'claude'])], runners)).toBe(
      true
    );
  });

  it('is false when the first available runner lacks chat', () => {
    expect(targetSupportsChat('flash', [model('flash', ['gemini', 'claude'])], runners)).toBe(
      false
    );
  });

  it('is false when the model has no available runner', () => {
    expect(targetSupportsChat('ghost', [model('ghost', ['offline'])], runners)).toBe(false);
  });

  it('resolves a bare tool name to itself', () => {
    expect(targetSupportsChat('codex', [], runners)).toBe(true);
    expect(targetSupportsChat('gemini', [], runners)).toBe(false);
  });

  it('is false with no runner data', () => {
    expect(targetSupportsChat('codex', undefined, undefined)).toBe(false);
  });
});
