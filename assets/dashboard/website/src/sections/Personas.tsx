import { useScrollReveal } from '../hooks/useScrollReveal';
import styles from '../styles/sections.module.css';

export default function Personas() {
  const ref = useScrollReveal();

  return (
    <section ref={ref} className={`${styles.section} website-reveal`}>
      <h2 className={styles.sectionTitle}>Personas</h2>
      <p className={styles.painPoint}>
        Three agents completed the same task. But one was a Security Auditor, one was a QA Engineer,
        one was your default coder. Each approached the work differently — not just different code,
        but different perspectives.
      </p>
      <img
        src="./screenshot-personas.png"
        alt="list of schmux persona"
        className={styles.screenshotLandscape}
        loading="lazy"
      />
    </section>
  );
}
