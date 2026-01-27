import React from 'react';
import { useNavigate } from 'react-router-dom';

type SyncResultModalProps = {
  success: boolean;
  message: string;
  navigateTo?: string;
  onClose: () => void;
};

export default function SyncResultModal({ success, message, navigateTo, onClose }: SyncResultModalProps) {
  const navigate = useNavigate();

  const handleClose = () => {
    onClose();
    if (navigateTo) {
      navigate(`/sessions/${navigateTo}`);
    }
  };

  const title = success ? 'Success' : 'Error';

  return (
    <div className="modal-overlay" role="dialog" aria-modal="true" aria-labelledby="sync-modal-title">
      <div className="modal">
        <div className="modal__header">
          <h2 className="modal__title" id="sync-modal-title">{title}</h2>
        </div>
        <div className="modal__body">
          <p>{message}</p>
        </div>
        <div className="modal__footer">
          <button className={`btn ${success ? 'btn--primary' : 'btn--danger'}`} onClick={handleClose}>
            OK
          </button>
        </div>
      </div>
    </div>
  );
}
