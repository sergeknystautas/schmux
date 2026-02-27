import { useState, useCallback } from 'react';
import { useScrollReveal } from '../hooks/useScrollReveal';
import { ClipboardIcon, CheckIcon } from '../icons';
import styles from '../styles/close.module.css';

const INSTALL_CMD =
  'curl -fsSL https://raw.githubusercontent.com/sergeknystautas/schmux/main/install.sh | bash';

const GITHUB_URL = 'https://github.com/sergeknystautas/schmux';
const PHILOSOPHY_URL = 'https://github.com/sergeknystautas/schmux/blob/main/docs/PHILOSOPHY.md';
const CONTRIBUTING_URL = 'https://github.com/sergeknystautas/schmux/blob/main/CONTRIBUTING.md';

export default function Close() {
  const ref = useScrollReveal();
  const [copied, setCopied] = useState(false);

  const handleCopy = useCallback(() => {
    navigator.clipboard.writeText(INSTALL_CMD).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    });
  }, []);

  return (
    <section ref={ref} className={`${styles.close} website-reveal`}>
      <div className={styles.inner}>
        <h2 className={styles.heading}>Get started</h2>
        <div className={styles.installBlock}>
          <pre className={styles.installCode}>{INSTALL_CMD}</pre>
          <button
            className={styles.copyButton}
            onClick={handleCopy}
            aria-label="Copy install command to clipboard"
          >
            {copied ? (
              <CheckIcon className={styles.copyIcon} />
            ) : (
              <ClipboardIcon className={styles.copyIcon} />
            )}
            {copied ? 'Copied' : 'Copy'}
          </button>
        </div>
        <div className={styles.links}>
          <a href={GITHUB_URL}>GitHub</a>
          <span className={styles.separator}>·</span>
          <a href={PHILOSOPHY_URL}>Philosophy</a>
          <span className={styles.separator}>·</span>
          <a href={CONTRIBUTING_URL}>Contributing</a>
        </div>
        <p className={styles.footer}>Built with schmux</p>
      </div>
    </section>
  );
}
