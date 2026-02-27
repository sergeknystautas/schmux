import { Link } from 'react-router-dom';
import styles from '../../styles/tips.module.css';

export function WorkflowTab() {
  return (
    <div>
      <h2>Workflow Guide</h2>
      <p>
        schmux is designed for running multiple agents in parallel on the same codebase. Each agent
        has strengths — use them together to get better results faster.
      </p>

      <h3>Multi-Agent Strategy</h3>
      <ul className={styles.tipsList}>
        <li>
          <strong>Parallel reviews:</strong> Spawn different agents (Claude, Kimi, Codex) on the
          same branch to get diverse perspectives
        </li>
        <li>
          <strong>Specialized tasks:</strong> Use fast models for quick edits, powerful models for
          complex refactoring
        </li>
        <li>
          <strong>Comparison:</strong> Use the diff viewer to compare approaches across workspaces
        </li>
      </ul>

      <h3>Choosing a Model</h3>
      <table className={styles.modelTable}>
        <thead>
          <tr>
            <th>Model</th>
            <th>Best For</th>
          </tr>
        </thead>
        <tbody>
          <tr>
            <td>
              <code>claude-opus-4-6</code>
            </td>
            <td>Complex reasoning, architecture, large refactors</td>
          </tr>
          <tr>
            <td>
              <code>claude-sonnet-4-6</code>
            </td>
            <td>Balanced speed/quality, feature work, debugging</td>
          </tr>
          <tr>
            <td>
              <code>claude-haiku-4-5</code>
            </td>
            <td>Quick edits, documentation, simple tasks</td>
          </tr>
          <tr>
            <td>
              <code>kimi-thinking</code>
            </td>
            <td>Deep analysis, code reviews, complex problems</td>
          </tr>
          <tr>
            <td>
              <code>glm-4.7</code>
            </td>
            <td>General coding, fast responses</td>
          </tr>
        </tbody>
      </table>

      <h3>Typical Workflow</h3>
      <ol className={styles.numberedList}>
        <li>
          <strong>Create a feature branch:</strong>{' '}
          <code>schmux spawn -r myrepo -b feature-x -t claude-haiku-4-5 -p "create feature X"</code>
        </li>
        <li>
          <strong>Let the agent work:</strong> Monitor via dashboard or attach with tmux to watch in
          real-time
        </li>
        <li>
          <strong>Review changes:</strong> Use the diff viewer to see what the agent did
        </li>
        <li>
          <strong>Spawn additional agents:</strong> Add reviewers or specialists to the same
          workspace
        </li>
        <li>
          <strong>Iterate:</strong> Provide feedback, let agents refine the work
        </li>
        <li>
          <strong>Commit and sync:</strong> When satisfied, commit changes and sync back to main
        </li>
      </ol>

      <h3>Branch Strategy</h3>
      <p>
        Each workspace uses a specific branch. This isolates work and makes it easy to compare
        approaches.
      </p>
      <table className={styles.modelTable}>
        <thead>
          <tr>
            <th>Strategy</th>
            <th>Use When</th>
          </tr>
        </thead>
        <tbody>
          <tr>
            <td>
              <strong>Feature branches</strong>
            </td>
            <td>One branch per feature, multiple agents can work in the same workspace</td>
          </tr>
          <tr>
            <td>
              <strong>Experiment branches</strong>
            </td>
            <td>Try different approaches in parallel workspaces</td>
          </tr>
          <tr>
            <td>
              <strong>Worktree mode</strong>
            </td>
            <td>Default — disk efficient, each branch can only be used by one workspace</td>
          </tr>
          <tr>
            <td>
              <strong>Full clone mode</strong>
            </td>
            <td>Multiple workspaces can use the same branch (uses more disk)</td>
          </tr>
        </tbody>
      </table>

      <h3>Git Lifecycle</h3>
      <p>
        schmux is built around a linear git workflow. Here's the expected lifecycle for a feature:
      </p>
      <ol className={styles.numberedList}>
        <li>
          <strong>Branch:</strong> Every <code>schmux spawn</code> creates an isolated workspace
          with its own feature branch. Agents work on their branch without stepping on each other.
        </li>
        <li>
          <strong>Work:</strong> Let agents make changes. Monitor via the dashboard, attach via tmux
          when you need to intervene.
        </li>
        <li>
          <strong>Stay current:</strong> Use "Sync from main" in the{' '}
          <Link to="/">git graph page</Link> to pull in all new commits from main into your branch.
          If there are conflicts, schmux uses an LLM to resolve them automatically.
        </li>
        <li>
          <strong>Ship:</strong> When ready, use "Sync to main" to fast-forward main to your branch.
          Or push your branch and create a PR externally.
        </li>
      </ol>
      <p>
        <em>
          schmux avoids merge commits. Sync-from-main cherry-picks, sync-to-main fast-forwards. Your
          history stays linear.
        </em>
      </p>
    </div>
  );
}
