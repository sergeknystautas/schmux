// Shared attachment helpers for the chat composer and the spawn form.

/** Largest file either attachment endpoint accepts. */
export const MAX_ATTACHMENT_BYTES = 50 * 1024 * 1024;

/**
 * Appends the "File attachments:" block that points an agent at uploaded
 * files. The daemon writes the same block for spawn prompts
 * (AppendFileList in internal/attachment/attachment.go).
 */
export function withFileAttachments(text: string, paths: string[]): string {
  if (paths.length === 0) return text;
  return `${text ? `${text}\n\n` : ''}File attachments:\n${paths.join('\n')}`;
}
