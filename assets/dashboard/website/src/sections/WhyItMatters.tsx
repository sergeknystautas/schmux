import { useScrollReveal } from '../hooks/useScrollReveal';
import styles from '../styles/why.module.css';

export default function WhyItMatters() {
  const ref = useScrollReveal();

  return (
    <section ref={ref} className={`${styles.why} website-reveal`}>
      <h2 className={styles.sectionTitle}>Why the outer loop matters</h2>
      <div className={styles.grid}>
        <div className={styles.block}>
          <h3>Compounding knowledge</h3>
          <p>
            Your agents make mistakes. They try <code>npm run build</code> when the answer is{' '}
            <code>go run ./cmd/build-dashboard</code>. They look for session code in the wrong
            package. These friction points are captured automatically, curated by an LLM, and
            proposed as updates to your project's instruction files — CLAUDE.md, AGENTS.md,
            .cursorrules, all of them. Every agent that runs makes the next one smarter. The
            knowledge compounds.
          </p>
        </div>
        <div className={styles.block}>
          <h3>Cross-model learning</h3>
          <p>
            Frontier labs will build agentic harnesses for their own models — but they'll never
            observe how a competitor's model works on your codebase. schmux can. Because it sits
            above all the agents, the lore system captures friction from Claude, Codex, Gemini, and
            model-agnostic harnesses alike. A lesson learned from one model's mistake gets baked
            into instruction files that help every other model. No single vendor can build this
            compounding loop across models — but you get it for free by running the factory.
          </p>
        </div>
      </div>
    </section>
  );
}
