import { useScrollReveal } from '../hooks/useScrollReveal';
import styles from '../styles/sections.module.css';

export default function Spawn() {
  const ref = useScrollReveal();

  return (
    <section ref={ref} className={`${styles.section} website-reveal`}>
      <h2 className={styles.sectionTitle}>Parallel spawning</h2>
      <div className={styles.inlineRow}>
        <p className={styles.painPoint}>
          You want Claude and Codex to both take a crack at the same feature. Setting up two copies
          of the repo, checking out branches, copying your .env files — that's a lot of grunt work
          before any agent can do any of their work.
        </p>
        <a href="demo/#/spawn" className="website-demo-card website-demo-card--sm">
          <span className="website-demo-card__icon">&#9654;</span>
          <span className="website-demo-card__content">
            <span className="website-demo-card__title">Interactive demo</span>
            <span className="website-demo-card__desc">Try the spawn wizard with sample repos</span>
          </span>
        </a>
      </div>
      <img
        src="./screenshot-spawn.png"
        alt="schmux spawn wizard for launching multiple agent sessions"
        className={styles.screenshotLandscape}
        loading="lazy"
      />
    </section>
  );
}
