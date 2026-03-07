import { useScrollReveal } from '../hooks/useScrollReveal';
import styles from '../styles/why.module.css';

export default function ModelCatalog() {
  const ref = useScrollReveal();

  return (
    <section ref={ref} className={`${styles.why} website-reveal`}>
      <h2 className={styles.sectionTitle}>Model catalog</h2>
      <div className={styles.grid}>
        <div className={styles.block}>
          <h3>Multi-provider</h3>
          <p>
            15+ models from Anthropic, Moonshot, ZAI, and MiniMax. Pick a model in the spawn wizard
            and schmux routes it through the right tool — Claude Code, Codex, Gemini CLI, or
            OpenCode — with the correct flags and API keys. You choose the model. schmux handles the
            plumbing.
          </p>
        </div>
        <div className={styles.block}>
          <h3>Custom targets</h3>
          <p>
            Define your own run targets with arbitrary CLI commands. If it runs in a terminal,
            schmux can orchestrate it. No vendor lock-in, no artificial limits on what counts as an
            "agent." Today's hot new coding tool is tomorrow's run target — add it in config and
            spawn away.
          </p>
        </div>
      </div>
    </section>
  );
}
