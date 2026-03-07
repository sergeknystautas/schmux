import { useScrollReveal } from '../hooks/useScrollReveal';
import styles from '../styles/sections.module.css';

export default function ConflictResolution() {
  const ref = useScrollReveal();

  return (
    <section ref={ref} className={`${styles.section} website-reveal`}>
      <h2 className={styles.sectionTitle}>Conflict resolution</h2>
      <p className={styles.painPoint}>
        Six agents, six branches, all touching the same files. Rebasing means conflict after
        conflict — and you're the one resolving them all by hand. The agents that wrote the code
        aren't even looking at the merge.
      </p>
      <p className={styles.solution}>
        Linear Sync brings in upstream commits one at a time. When conflicts arise, an LLM reads
        both sides, understands intent, and produces clean merges. You see confidence scores and
        per-file actions before anything lands.
      </p>
    </section>
  );
}
