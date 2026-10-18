import type { ReactNode, RefObject } from 'react';

/** Shared presentation for ModalProvider and dialogs with interactive content.
 * Callers own focus trapping and keyboard handling for their dialog's lifecycle.
 */
export default function ModalFrame({
  title,
  titleId,
  modalRef,
  wide = false,
  testId,
  children,
  footer,
}: {
  title: ReactNode;
  titleId: string;
  modalRef: RefObject<HTMLDivElement | null>;
  wide?: boolean;
  testId?: string;
  children: ReactNode;
  footer: ReactNode;
}) {
  return (
    <div className="modal-overlay" role="dialog" aria-modal="true" aria-labelledby={titleId}>
      <div ref={modalRef} className={`modal${wide ? ' modal--wide' : ''}`} data-testid={testId}>
        <div className="modal__header">
          <h2 className="modal__title" id={titleId}>
            {title}
          </h2>
        </div>
        <div className="modal__body">{children}</div>
        <div className="modal__footer">{footer}</div>
      </div>
    </div>
  );
}
