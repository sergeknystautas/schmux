import { useEffect, useState, useRef, useCallback } from 'react';
import useLocalStorage from './useLocalStorage';

const DEFAULT_SIDEBAR_WIDTH = 300;
const MIN_SIDEBAR_WIDTH = 150;
const MAX_SIDEBAR_WIDTH = 600;

interface UseSidebarLayoutOptions {
  /** localStorage key for persisting sidebar width */
  widthKey: string;
  /** localStorage key for persisting keyboard focus panel */
  focusKey: string;
  /** Total number of files in the list */
  fileCount: number;
  /** Currently selected file index */
  selectedFileIndex: number;
  /** Callback when selected file index changes */
  onSelectFile: (index: number) => void;
  /** Enable vim-style j/k keys for file navigation */
  vimKeys?: boolean;
}

/**
 * Hook that manages sidebar resize, keyboard navigation, and focus state
 * for file-list + diff-viewer layouts (DiffPage, CommitDetailPage).
 */
export default function useSidebarLayout(opts: UseSidebarLayoutOptions) {
  const { widthKey, focusKey, fileCount, selectedFileIndex, onSelectFile, vimKeys } = opts;

  const [sidebarWidth, setSidebarWidth] = useLocalStorage<number>(widthKey, DEFAULT_SIDEBAR_WIDTH);
  const [isResizing, setIsResizing] = useState(false);
  const [keyboardFocus, setKeyboardFocus] = useLocalStorage<'left' | 'right' | null>(
    focusKey,
    'left'
  );
  const containerRef = useRef<HTMLDivElement>(null);
  const contentRef = useRef<HTMLDivElement>(null);

  // --- Resize ---

  const handleMouseDown = useCallback((e: React.MouseEvent) => {
    e.preventDefault();
    setIsResizing(true);
  }, []);

  const handleMouseMove = useCallback(
    (e: MouseEvent) => {
      if (!isResizing || !containerRef.current) return;
      const containerRect = containerRef.current.getBoundingClientRect();
      const newWidth = e.clientX - containerRect.left;
      const clampedWidth = Math.max(MIN_SIDEBAR_WIDTH, Math.min(MAX_SIDEBAR_WIDTH, newWidth));
      setSidebarWidth(clampedWidth);
    },
    [isResizing, setSidebarWidth]
  );

  const handleMouseUp = useCallback(() => {
    setIsResizing(false);
  }, []);

  useEffect(() => {
    if (isResizing) {
      document.addEventListener('mousemove', handleMouseMove);
      document.addEventListener('mouseup', handleMouseUp);
      document.body.style.cursor = 'col-resize';
      document.body.style.userSelect = 'none';
    }
    return () => {
      document.removeEventListener('mousemove', handleMouseMove);
      document.removeEventListener('mouseup', handleMouseUp);
      document.body.style.cursor = '';
      document.body.style.userSelect = '';
    };
  }, [isResizing, handleMouseMove, handleMouseUp]);

  // --- Keyboard navigation ---

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      switch (e.key) {
        case 'ArrowLeft':
          e.preventDefault();
          setKeyboardFocus('left');
          break;
        case 'ArrowRight':
          e.preventDefault();
          setKeyboardFocus('right');
          break;
        case 'ArrowUp':
        case vimKeys && e.key === 'k' ? 'k' : undefined:
          if (keyboardFocus === 'left' && selectedFileIndex > 0) {
            e.preventDefault();
            onSelectFile(selectedFileIndex - 1);
          } else if (keyboardFocus === 'right' && contentRef.current) {
            e.preventDefault();
            contentRef.current.scrollBy({ top: -100, behavior: 'smooth' });
          }
          break;
        case 'ArrowDown':
        case vimKeys && e.key === 'j' ? 'j' : undefined:
          if (keyboardFocus === 'left' && selectedFileIndex < fileCount - 1) {
            e.preventDefault();
            onSelectFile(selectedFileIndex + 1);
          } else if (keyboardFocus === 'right' && contentRef.current) {
            e.preventDefault();
            contentRef.current.scrollBy({ top: 100, behavior: 'smooth' });
          }
          break;
      }
    };

    document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, [keyboardFocus, selectedFileIndex, fileCount, onSelectFile, setKeyboardFocus, vimKeys]);

  // --- Auto-scroll sidebar ---

  useEffect(() => {
    if (keyboardFocus === 'left') {
      const activeFileEl = document.querySelector('.diff-file-item--active');
      if (activeFileEl) {
        activeFileEl.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
      }
    }
  }, [selectedFileIndex, keyboardFocus]);

  // --- Focus handlers ---

  const handleSidebarFocus = useCallback(() => setKeyboardFocus('left'), [setKeyboardFocus]);
  const handleContentFocus = useCallback(() => setKeyboardFocus('right'), [setKeyboardFocus]);

  return {
    sidebarWidth,
    isResizing,
    keyboardFocus,
    containerRef,
    contentRef,
    handleMouseDown,
    handleSidebarFocus,
    handleContentFocus,
  };
}
