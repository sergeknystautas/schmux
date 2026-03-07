import { useScrollReveal } from '../hooks/useScrollReveal';
import styles from '../styles/sections.module.css';

export default function Overlays() {
  const ref = useScrollReveal();

  return (
    <section ref={ref} className={`${styles.section} website-reveal`}>
      <h2 className={styles.sectionTitle}>Workspace overlays</h2>
      <p className={styles.painPoint}>
        Your .env has database credentials, API keys, and feature flags. Every new workspace needs a
        copy. By the fifth workspace today, you've fat-fingered a paste and your tests are failing
        against the wrong database.
      </p>
      <p className={styles.solution}>
        Drop files into <code>~/.schmux/overlays/&lt;repo&gt;/</code> and they're copied into every
        new workspace automatically. schmux enforces that overlay files are covered by .gitignore —
        secrets never get committed by accident.
      </p>
    </section>
  );
}
