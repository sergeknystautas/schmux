import { useScrollReveal } from '../hooks/useScrollReveal';
import styles from '../styles/sections.module.css';

export default function Sessions() {
  const ref = useScrollReveal();

  return (
    <section ref={ref} className={`${styles.section} website-reveal`}>
      <div className={styles.layout}>
        <div className={styles.text}>
          <h2 className={styles.sectionTitle}>Agent status</h2>
          <p className={styles.painPoint}>
            You have 6 agents running. Two are done, one is stuck waiting for permission, one has a
            question, two are still working. Get notified immediately when an agent needs your
            attention.
          </p>
          <a href="demo/#/workspaces" className="website-demo-card">
            <span className="website-demo-card__icon">&#9654;</span>
            <span className="website-demo-card__content">
              <span className="website-demo-card__title">Interactive demo</span>
              <span className="website-demo-card__desc">
                Walk through agent monitoring with sample data — no install needed
              </span>
            </span>
          </a>
        </div>
        <img
          src="./screenshot-sessions.png"
          alt="schmux dashboard showing session list with agent statuses"
          className={styles.screenshotPortrait}
          loading="lazy"
        />
      </div>
    </section>
  );
}
