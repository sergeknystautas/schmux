import React from 'react';

type PastebinTabProps = {
  pastebin: string[];
  dispatch: React.Dispatch<import('./useConfigForm').ConfigFormAction>;
  onOpenPastebinEditModal: (index: number, content: string) => void;
  onOpenAddPastebinModal: () => void;
};

export default function PastebinTab({
  pastebin,
  dispatch,
  onOpenPastebinEditModal,
  onOpenAddPastebinModal,
}: PastebinTabProps) {
  return (
    <div className="wizard-step-content" data-step="4">
      <h2 className="wizard-step-content__title">Pastebin</h2>
      <p className="wizard-step-content__description">
        Saved text clips you can paste into any active terminal session.
      </p>

      <div className="settings-section">
        <div className="settings-section__header">
          <h3>Clips</h3>
        </div>
        <div className="settings-section__body">
          {pastebin.length === 0 ? (
            <div className="empty-state-hint">No clips yet.</div>
          ) : (
            <div className="item-list">
              {pastebin.map((content, index) => (
                <div className="item-list__item" key={index}>
                  <div className="item-list__item-content">
                    <pre style={{ margin: 0, whiteSpace: 'pre-wrap', fontSize: '0.85rem' }}>
                      {content.length > 100 ? content.slice(0, 100) + '...' : content}
                    </pre>
                  </div>
                  <div className="btn-group">
                    <button
                      className="btn btn--sm btn--primary"
                      onClick={() => onOpenPastebinEditModal(index, content)}
                    >
                      Edit
                    </button>
                    <button
                      className="btn btn--sm btn--danger"
                      onClick={() => dispatch({ type: 'REMOVE_PASTEBIN', index })}
                    >
                      Remove
                    </button>
                  </div>
                </div>
              ))}
            </div>
          )}
          <button className="btn btn--primary" onClick={onOpenAddPastebinModal}>
            Add clip
          </button>
        </div>
      </div>
    </div>
  );
}
