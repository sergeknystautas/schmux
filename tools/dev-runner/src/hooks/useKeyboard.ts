import { useInput } from 'ink';

interface UseKeyboardOptions {
  onRestart: () => void;
  onClear: () => void;
  onQuit: () => void;
  onToggleLayout: () => void;
  canRestart: boolean;
}

export function useKeyboard({
  onRestart,
  onClear,
  onQuit,
  onToggleLayout,
  canRestart,
}: UseKeyboardOptions): void {
  useInput((input) => {
    if (input === 'r' && canRestart) {
      onRestart();
    } else if (input === 'c') {
      onClear();
    } else if (input === 'l') {
      onToggleLayout();
    } else if (input === 'q') {
      onQuit();
    }
  });
}
