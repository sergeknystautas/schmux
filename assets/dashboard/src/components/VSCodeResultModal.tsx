import React from 'react';

type VSCodeResultModalProps = {
  success: boolean;
  message: string;
  onClose: () => void;
};

export default function VSCodeResultModal({ success, message, onClose }: VSCodeResultModalProps) {
  const title = success ? 'VS Code opened' : 'Unable to open VS Code';
  const lines = message.split('\n');

  return (
    <div className="modal-overlay" role="dialog" aria-modal="true" aria-labelledby="vscode-modal-title">
      <div className="modal">
        <div className="modal__header">
          <h2 className="modal__title" id="vscode-modal-title">{title}</h2>
        </div>
        <div className="modal__body">
          {lines.map((line, i) => (
            <p key={i} style={{ margin: i === 0 ? '0 0 0.5rem 0' : '0 0 0.5rem 0' }}>{line}</p>
          ))}
        </div>
        <div className="modal__footer">
          <button className="btn btn--primary" onClick={onClose}>
            OK
          </button>
        </div>
      </div>
    </div>
  );
}
