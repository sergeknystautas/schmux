import { useScrollReveal } from '../hooks/useScrollReveal';
import styles from '../styles/sections.module.css';

export default function Digest() {
  const ref = useScrollReveal();

  return (
    <section ref={ref} className={`${styles.section} website-reveal`}>
      <h2 className={styles.sectionTitle}>Activity digest</h2>
      <p className={styles.painPoint}>
        Twelve agents ran overnight. You have 47 new commits across 8 branches. You could read each
        diff, or you could read the three-paragraph summary that tells you what actually happened.
      </p>
      <p className={styles.solution}>
        Every few hours, an LLM reads recent commits across all your workspaces and writes a casual,
        readable digest — what got built, what got stuck, and what needs your attention. It shows up
        on your home page so you're caught up before you even open a terminal.
      </p>
    </section>
  );
}
