import { useScrollReveal } from '../hooks/useScrollReveal';
import styles from '../styles/code-review.module.css';

export default function CodeReview() {
  const ref = useScrollReveal();

  return (
    <section ref={ref} className={`${styles.review} website-reveal`}>
      <h2 className={styles.sectionTitle}>Review what they built</h2>
      <div className={styles.grid}>
        <div className={styles.block}>
          <h3>Diff viewer</h3>
          <p>
            Side-by-side diffs with a file list, line counts, and binary file detection. Click any
            file to open it in VS Code. See exactly what each agent changed without leaving the
            dashboard.
          </p>
        </div>
        <div className={styles.block}>
          <h3>Commit graph</h3>
          <p>
            Interactive branch topology — see where agents forked, what they committed, and how
            branches relate. Cherry-pick individual commits and push to remote straight from the
            graph.
          </p>
        </div>
        <div className={styles.block}>
          <h3>PR integration</h3>
          <p>
            Open pull requests appear on your home page. Check one out and schmux creates a
            workspace, checks out the branch, and spawns a review session. One click from PR to
            running agent.
          </p>
        </div>
      </div>
    </section>
  );
}
