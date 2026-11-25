import { Link } from 'react-router';
import styles from '../../styles/tips.module.css';

export function PowerToolsTab() {
  return (
    <div>
      <h2>Power Tools</h2>
      <p>
        Advanced features for power users. These are harder to discover but can significantly
        improve your workflow.
      </p>

      <h3>Autolearn (Continual Learning)</h3>
      <p>
        As agents work, they write down things they learn — workarounds, codebase quirks,
        configuration gotchas — to <code>.schmux/autolearn.jsonl</code> in each workspace.
      </p>

      <h4>What schmux does with it</h4>
      <ul className={styles.tipsList}>
        <li>
          <strong>Curation:</strong> When sessions or workspaces are disposed, an LLM curator
          reviews the raw signals and extracts useful learnings
        </li>
        <li>
          <strong>Proposals:</strong> Proposals appear for you to review, apply, or dismiss
        </li>
        <li>
          <strong>Applied rules:</strong> Gets committed to your repo's instruction files (like{' '}
          <code>CLAUDE.md</code>), so future agents benefit
        </li>
      </ul>
      <p>
        <em>
          Configure in the <Link to="/config">config page</Link> (autolearn section) — set which LLM
          curates, when curation triggers, and which instruction files to update.
        </em>
      </p>

      <h3>GitHub PR Review</h3>
      <p>
        schmux can discover open pull requests on your configured repos and help you review them
        with an AI agent.
      </p>

      <h4>How it works</h4>
      <ol className={styles.numberedList}>
        <li>schmux polls GitHub for open PRs on your repos (hourly, up to 5 per repo)</li>
        <li>PRs appear on the home page</li>
        <li>
          Click a PR to check it out into a new workspace and spawn a review session with full PR
          context
        </li>
      </ol>
      <p>
        <em>
          Setup: Run <code>schmux auth github</code> to configure GitHub access, then set a{' '}
          <code>pr_review.target</code> in your <Link to="/config">config</Link> to define which LLM
          reviews PRs.
        </em>
      </p>
    </div>
  );
}
