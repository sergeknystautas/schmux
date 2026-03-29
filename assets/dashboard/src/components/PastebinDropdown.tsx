import React from 'react';
import { useNavigate } from 'react-router-dom';
import styles from './ActionDropdown.module.css';

type PastebinDropdownProps = {
  entries: string[];
  onPaste: (content: string) => void;
  onClose: () => void;
  disabled?: boolean;
};

export default function PastebinDropdown({
  entries,
  onPaste,
  onClose,
  disabled,
}: PastebinDropdownProps) {
  const navigate = useNavigate();

  const handleManage = (e: React.MouseEvent) => {
    e.stopPropagation();
    onClose();
    navigate('/config?tab=pastebin');
  };

  const handlePaste = (e: React.MouseEvent, content: string) => {
    e.stopPropagation();
    onPaste(content);
    onClose();
  };

  return (
    <div className={styles.menu} role="menu">
      <div className={styles.sectionHeader}>
        <span className={styles.sectionLabel}>Pastebin</span>
        <button className={styles.manageLink} onClick={handleManage}>
          manage
        </button>
      </div>
      {!disabled && entries.length > 0 ? (
        entries.map((content, index) => (
          <button
            key={index}
            className={styles.item}
            onClick={(e) => handlePaste(e, content)}
            role="menuitem"
          >
            <span className={styles.itemLabel}>
              {content.split('\n')[0].length > 40
                ? content.split('\n')[0].slice(0, 40) + '...'
                : content.split('\n')[0]}
            </span>
          </button>
        ))
      ) : (
        <div className={styles.emptyState}>{disabled ? 'No active session' : 'No pastes yet'}</div>
      )}
    </div>
  );
}
