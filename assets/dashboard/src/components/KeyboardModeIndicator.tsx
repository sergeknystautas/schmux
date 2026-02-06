import React, { useEffect, useState } from 'react';
import { useKeyboardMode } from '../contexts/KeyboardContext';

export default function KeyboardModeIndicator() {
  const { mode } = useKeyboardMode();

  // Option 1: Blue border around viewport
  useEffect(() => {
    if (mode === 'active') {
      document.body.classList.add('keyboard-mode-active');
    } else {
      document.body.classList.remove('keyboard-mode-active');
    }
  }, [mode]);

  // The pills (options 2 and 3) are rendered in AppShell where the layout exists
  return null;
}
