/**
 * Strip ANSI escape sequences from a string, returning plain text.
 * Handles CSI sequences (colors, cursor movement), OSC sequences (hyperlinks),
 * and other common terminal escape codes.
 */
export function stripAnsi(str: string): string {
  return (
    str
      // CSI sequences: ESC [ ... (letter or @)
      .replace(/\x1b\[[0-9;]*[A-Za-z@]/g, '')
      // OSC sequences: ESC ] ... (ST or BEL)
      .replace(/\x1b\].*?(?:\x1b\\|\x07)/g, '')
      // ESC followed by single character (e.g., ESC c for reset)
      .replace(/\x1b[^[\]]/g, '')
  );
}
