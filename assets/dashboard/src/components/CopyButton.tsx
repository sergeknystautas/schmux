import Tooltip from './Tooltip';
import { useToast } from './ToastProvider';
import { copyToClipboard } from '../lib/utils';
import { CopyIcon } from './Icons';

type CopyButtonProps = {
  /** Exact text written to the clipboard. */
  text: string;
  /** What is copied, as read by the user: "Copy {label}" / "Copied {label}". */
  label: string;
  className?: string;
  testId?: string;
};

/**
 * Icon button that copies `text` and toasts the result. Every copy icon in the
 * dashboard uses it so tooltip, toast, and failure feedback stay identical.
 * Hover-revealed placement comes from the caller's className (`hover-copy__btn`
 * inside a `.hover-copy` host, or `copy-field__btn` inside a `.copy-field`).
 */
export default function CopyButton({
  text,
  label,
  className = 'icon-btn',
  testId,
}: CopyButtonProps) {
  const { success, error } = useToast();

  const handleCopy = async () => {
    if (await copyToClipboard(text)) {
      success(`Copied ${label}`);
    } else {
      error('Failed to copy');
    }
  };

  return (
    <Tooltip content={`Copy ${label}`}>
      <button
        type="button"
        className={className}
        aria-label={`Copy ${label}`}
        data-testid={testId}
        onClick={handleCopy}
      >
        {CopyIcon}
      </button>
    </Tooltip>
  );
}
