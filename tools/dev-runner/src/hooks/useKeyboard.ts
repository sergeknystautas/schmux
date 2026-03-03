import { useInput } from 'ink';

interface UseKeyboardOptions {
  onRestart: () => void;
  onPull: () => void;
  onClear: () => void;
  onQuit: () => void;
  onToggleLayout: () => void;
  onResetWorkspace: () => void;
  canRestart: boolean;
  canResetWorkspace: boolean;
}

export function useKeyboard({
  onRestart,
  onPull,
  onClear,
  onQuit,
  onToggleLayout,
  onResetWorkspace,
  canRestart,
  canResetWorkspace,
}: UseKeyboardOptions): void {
  useInput((input) => {
    if (input === 'r' && canRestart) {
      onRestart();
    } else if (input === 'p' && canRestart) {
      onPull();
    } else if (input === 'w' && canResetWorkspace) {
      onResetWorkspace();
    } else if (input === 'c') {
      onClear();
    } else if (input === 'l') {
      onToggleLayout();
    } else if (input === 'q') {
      onQuit();
    }
  });
}
