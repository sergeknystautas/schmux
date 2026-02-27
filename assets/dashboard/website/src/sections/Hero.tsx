import styles from '../styles/hero.module.css';

export default function Hero() {
  return (
    <section className={styles.hero}>
      <h1 className={styles.title}>schmux</h1>
      <p className={styles.tagline}>Factory-floor tooling for AI-assisted software development.</p>
      <p className={styles.description}>
        Run multiple AI coding agents in parallel, each in its own
        <br />
        isolated version-controlled workspace, all visible from one dashboard.
      </p>
      <img
        src="./screenshot.png"
        alt="schmux dashboard showing multiple AI agent sessions running in parallel"
        className={styles.screenshot}
        loading="lazy"
      />
    </section>
  );
}
