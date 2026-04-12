import { Link } from 'react-router-dom';
import styles from '../../styles/tips.module.css';

export function PowerToolsTab() {
  return (
    <div>
      <h2>Power Tools</h2>
      <p>
        Advanced features for power users. These are harder to discover but can significantly
        improve your workflow.
      </p>

      <h3>Remote Hosts</h3>
      <p>
        Run agents on remote machines. schmux is transport-agnostic — you define the commands to
        connect, provision, and reconnect. SSH is the most common choice, but any command that gives
        you a shell works.
      </p>

      <h4>How it works</h4>
      <ol className={styles.numberedList}>
        <li>
          Configure a remote "flavor" in <Link to="/config?tab=remote">Settings &gt; Remote</Link> —
          define your connect command, provision command, and reconnect command
        </li>
        <li>When spawning a session, select your remote flavor as the target environment</li>
        <li>
          schmux runs your commands to get a working environment on the remote host, then streams
          the terminal back to your dashboard
        </li>
      </ol>

      <h4>What you get</h4>
      <ul className={styles.tipsList}>
        <li>
          <strong>Live terminal streaming:</strong> Remote sessions stream to your dashboard, same
          as local
        </li>
        <li>
          <strong>NudgeNik:</strong> Status detection works on remote sessions
        </li>
        <li>
          <strong>VS Code:</strong> Remote launch if configured in the flavor
        </li>
      </ul>

      <h3>Lore (Continual Learning)</h3>
      <p>
        As agents work, they write down things they learn — workarounds, codebase quirks,
        configuration gotchas — to <code>.schmux/lore.jsonl</code> in each workspace.
      </p>

      <h4>What schmux does with it</h4>
      <ul className={styles.tipsList}>
        <li>
          <strong>Curation:</strong> When sessions or workspaces are disposed, an LLM curator
          reviews the raw lore and extracts useful learnings
        </li>
        <li>
          <strong>Proposals:</strong> Proposals appear for you to review, apply, or dismiss
        </li>
        <li>
          <strong>Applied lore:</strong> Gets committed to your repo's instruction files (like{' '}
          <code>CLAUDE.md</code>), so future agents benefit
        </li>
      </ul>
      <p>
        <em>
          Configure in the <Link to="/config">config page</Link> (lore section) — set which LLM
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
