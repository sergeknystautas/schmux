import styles from './FileDropOverlay.module.css';

/** Positions a drop target so FileDropOverlay can frame it. */
export const dropZoneClassName = styles.zone;

/** Outline and prompt shown while files are dragged over a drop target. */
export default function FileDropOverlay({ testId }: { testId: string }) {
  return (
    <div className={styles.fileDropOverlay} data-testid={testId}>
      <div className={styles.fileDropPrompt} role="status">
        Drop files to attach
      </div>
    </div>
  );
}
