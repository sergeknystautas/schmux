import React from 'react';

export default function SetupCompleteModal({ onClose }) {
  return (
    <div className="modal-overlay" role="dialog" aria-modal="true" aria-labelledby="setup-modal-title">
      <div className="modal">
        <div className="modal__header">
          <h2 className="modal__title" id="setup-modal-title">Setup Complete! 🎉</h2>
        </div>
        <div className="modal__body">
          <p>schmux is ready to go. Spawn your first session to start working with AI agents.</p>
        </div>
        <div className="modal__footer">
          <button className="btn btn--primary" onClick={onClose}>
            Go to Spawn
          </button>
        </div>
      </div>
    </div>
  );
}
