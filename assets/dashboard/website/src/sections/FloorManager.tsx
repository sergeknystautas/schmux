import { useScrollReveal } from '../hooks/useScrollReveal';
import styles from '../styles/sections.module.css';
import fm from '../styles/floor-manager.module.css';

export default function FloorManager() {
  const ref = useScrollReveal();

  return (
    <section ref={ref} className={`${styles.section} website-reveal`}>
      <div className={styles.layout}>
        <div className={styles.text}>
          <h2 className={styles.sectionTitle}>Floor manager</h2>
          <p className={styles.painPoint}>
            Switching between terminals to check on agents, approve requests, and kick off new tasks
            gets old fast. The floor manager gives you one place to talk to all of them — a single
            conversational interface for your whole factory.
          </p>
          <p className={fm.phoneHint}>This is how it looks like on your phone.</p>
        </div>
        <img
          src="./screenshot-floor-manager.png"
          alt="Floor manager chat interface showing a conversation about managing agent sessions"
          className={styles.screenshotPortrait}
          loading="lazy"
        />
      </div>
    </section>
  );
}
