export interface TerminalFrame {
  /** Milliseconds to wait before displaying this frame (relative to previous frame) */
  delay: number;
  /** Raw text to write to xterm.js, may contain ANSI escape codes */
  data: string;
}

export interface TerminalRecording {
  sessionId: string;
  frames: TerminalFrame[];
}
