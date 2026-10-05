import { useCallback, useRef, useState } from 'react';
import type { ChatImage } from '../lib/chat/types';
import { getErrorMessage } from '../lib/api';
import { MAX_ATTACHMENT_BYTES } from '../lib/attachments';

export interface UseAttachmentsOptions<F> {
  /** Stores one non-image file and returns its record. */
  upload(file: File): Promise<F>;
  initialImages?: ChatImage[];
  initialFiles?: F[];
  /** Images beyond this count are rejected with an error. */
  maxImages?: number;
  disabled?: boolean;
}

export interface Attachments<F> {
  images: ChatImage[];
  files: F[];
  attaching: boolean;
  error: string | null;
  attachFiles(files: Iterable<File>): Promise<void>;
  removeImage(index: number): void;
  removeFile(index: number): void;
  restore(images: ChatImage[], files: F[]): void;
  clear(): void;
}

function readFileAsImage(file: File): Promise<ChatImage> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => {
      const url = String(reader.result);
      const base64 = url.slice(url.indexOf(',') + 1);
      resolve({ media_type: file.type || 'image/png', data: base64 });
    };
    reader.onerror = () => reject(reader.error);
    reader.readAsDataURL(file);
  });
}

/**
 * Attachment state shared by the chat composer and the spawn form: images are
 * read inline with their media type, other files go through `upload`.
 */
export function useAttachments<F>({
  upload,
  initialImages,
  initialFiles,
  maxImages,
  disabled = false,
}: UseAttachmentsOptions<F>): Attachments<F> {
  const [images, setImages] = useState<ChatImage[]>(initialImages ?? []);
  const [files, setFiles] = useState<F[]>(initialFiles ?? []);
  const [attaching, setAttaching] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const uploadRef = useRef(upload);
  uploadRef.current = upload;
  const imageCountRef = useRef(images.length);
  imageCountRef.current = images.length;

  const attachFiles = useCallback(
    async (selectedFiles: Iterable<File>) => {
      if (disabled || attaching) return;
      // Snapshot the selection before the caller clears a native file input.
      const selection = Array.from(selectedFiles);
      setAttaching(true);
      setError(null);
      let imageCount = imageCountRef.current;
      try {
        for (const file of selection) {
          try {
            if (file.type.startsWith('image/')) {
              if (maxImages !== undefined && imageCount >= maxImages) {
                throw new Error(`maximum ${maxImages} images`);
              }
              imageCount += 1;
              const img = await readFileAsImage(file);
              setImages((prev) => [...prev, img]);
            } else {
              if (file.size > MAX_ATTACHMENT_BYTES) throw new Error('File exceeds 50 MiB');
              const record = await uploadRef.current(file);
              setFiles((prev) => [...prev, record]);
            }
          } catch (err) {
            setError(`${file.name}: ${getErrorMessage(err, 'Failed to attach file')}`);
          }
        }
      } finally {
        setAttaching(false);
      }
    },
    [attaching, disabled, maxImages]
  );

  const removeImage = useCallback(
    (index: number) => setImages((prev) => prev.filter((_, i) => i !== index)),
    []
  );
  const removeFile = useCallback(
    (index: number) => setFiles((prev) => prev.filter((_, i) => i !== index)),
    []
  );
  const restore = useCallback((nextImages: ChatImage[], nextFiles: F[]) => {
    setImages(nextImages);
    setFiles(nextFiles);
  }, []);
  const clear = useCallback(() => {
    setImages([]);
    setFiles([]);
    setError(null);
  }, []);

  return {
    images,
    files,
    attaching,
    error,
    attachFiles,
    removeImage,
    removeFile,
    restore,
    clear,
  };
}
