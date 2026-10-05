import { useCallback, useRef, useState } from 'react';
import type React from 'react';

const hasFiles = (types: readonly string[]) => Array.from(types).includes('Files');

/**
 * File drag-and-drop for a region: counts nested enter/leave pairs so moving
 * across child elements never flickers the overlay, and leaves text drops
 * native.
 */
export function useFileDrop({
  available,
  onFiles,
}: {
  available: boolean;
  onFiles(files: File[]): void;
}) {
  const [dragging, setDragging] = useState(false);
  const depthRef = useRef(0);
  const cancel = useCallback(() => {
    depthRef.current = 0;
    setDragging(false);
  }, []);

  const handlers = {
    onDragEnter: (event: React.DragEvent) => {
      if (!hasFiles(event.dataTransfer.types)) return;
      event.preventDefault();
      depthRef.current += 1;
      setDragging(true);
    },
    onDragOver: (event: React.DragEvent) => {
      if (!hasFiles(event.dataTransfer.types)) return;
      event.preventDefault();
      event.dataTransfer.dropEffect = available ? 'copy' : 'none';
    },
    onDragLeave: (event: React.DragEvent) => {
      if (!hasFiles(event.dataTransfer.types)) return;
      depthRef.current = Math.max(0, depthRef.current - 1);
      if (depthRef.current === 0) setDragging(false);
    },
    onDragEnd: cancel,
    onDrop: (event: React.DragEvent) => {
      if (!hasFiles(event.dataTransfer.types)) return;
      event.preventDefault();
      const files = Array.from(event.dataTransfer.files);
      cancel();
      if (available) onFiles(files);
    },
  };

  return { dragging, showOverlay: dragging && available, cancel, handlers };
}
