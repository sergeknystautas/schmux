import { useInput } from 'ink';

interface UseKeyboardOptions {
  onRestart: () => void;
  onPull: () => void;
  onClear: () => void;
  onQuit: () => void;
  onToggleLayout: () => void;
  canRestart: boolean;
}

export function useKeyboard({
  onRestart,
  onPull,
  onClear,
  onQuit,
  onToggleLayout,
  canRestart,
}: UseKeyboardOptions): void {
  useInput((input) => {
    if (input === 'r' && canRestart) {
      onRestart();
    } else if (input === 'p' && canRestart) {
      onPull();
    } else if (input === 'c') {
      onClear();
    } else if (input === 'l') {
      onToggleLayout();
    } else if (input === 'q') {
      onQuit();
    }
  });
}
