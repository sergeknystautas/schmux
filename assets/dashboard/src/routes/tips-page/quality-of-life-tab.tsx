import { Link } from 'react-router-dom';
import styles from '../../styles/tips.module.css';

export function QualityOfLifeTab() {
  return (
    <div>
      <h2>Quality of Life Features</h2>
      <p>
        schmux is highly focused on developer ergonomics. We've built numerous features to reduce
        friction and keep you in the flow.
      </p>

      <h3>NudgeNik</h3>
      <p>
        NudgeNik reads agent output and classifies their state so you can focus on what needs
        attention:
      </p>

      <table className={styles.modelTable}>
        <thead>
          <tr>
            <th>State</th>
            <th>Meaning</th>
          </tr>
        </thead>
        <tbody>
          <tr>
            <td>
              <strong>Blocked</strong>
            </td>
            <td>Agent needs permission to run a command or approve a change</td>
          </tr>
          <tr>
            <td>
              <strong>Waiting</strong>
            </td>
            <td>Agent has a question or needs user input</td>
          </tr>
          <tr>
            <td>
              <strong>Working</strong>
            </td>
            <td>Agent is actively making progress</td>
          </tr>
          <tr>
            <td>
              <strong>Done</strong>
            </td>
            <td>Agent completed all work</td>
          </tr>
        </tbody>
      </table>

      <p>
        Status appears on session tabs throughout the dashboard, helping you triage where to focus.
      </p>

      <h3>Quick Launch Presets</h3>
      <p>Save common combinations of target + prompt for one-click execution:</p>

      <ul className={styles.tipsList}>
        <li>
          <strong>Define presets:</strong> Add to <code>~/.schmux/config.json</code> (global) or
          workspace <code>.schmux/config.json</code> (repo-specific)
        </li>
        <li>
          <strong>Access anywhere:</strong> Appears in spawn dropdown and spawn wizard
        </li>
        <li>
          <strong>Perfect for repetitive tasks:</strong> "Run tests", "Commit changes", "Review PR",
          "Fix failing tests"
        </li>
      </ul>

      <h3>Git Integration</h3>
      <p>schmux provides quality-of-life features for git workflows:</p>

      <table className={styles.modelTable}>
        <thead>
          <tr>
            <th>Feature</th>
            <th>Description</th>
          </tr>
        </thead>
        <tbody>
          <tr>
            <td>
              <strong>Sync from main</strong>
            </td>
            <td>Cherry-pick main commits into your branch (no merge commits)</td>
          </tr>
          <tr>
            <td>
              <strong>Sync to main</strong>
            </td>
            <td>Fast-forward your branch directly to main</td>
          </tr>
          <tr>
            <td>
              <strong>Diff viewer</strong>
            </td>
            <td>Side-by-side diff in dashboard or external tools (VS Code, Kaleidoscope)</td>
          </tr>
          <tr>
            <td>
              <strong>VS Code</strong>
            </td>
            <td>One-click launch in any workspace</td>
          </tr>
          <tr>
            <td>
              <strong>Safety checks</strong>
            </td>
            <td>Can't dispose workspaces with uncommitted/unpushed changes</td>
          </tr>
        </tbody>
      </table>

      <h3>Tips & Tricks</h3>
      <ul className={styles.tipsList}>
        <li>
          <strong>Copy attach command:</strong> Session detail page has a button to copy the exact
          tmux attach command
        </li>
        <li>
          <strong>Bulk spawn:</strong> Spawn multiple agents at once from the spawn wizard
        </li>
        <li>
          <strong>Nicknames:</strong> Give sessions nicknames to easily identify them (e.g.,
          "reviewer", "fixer")
        </li>
        <li>
          <strong>JSON output:</strong> Use <code>--json</code> flag with CLI commands for scripting
        </li>
        <li>
          <strong>Workspace config:</strong> Add repo-specific quick launch presets in{' '}
          <code>&lt;workspace&gt;/.schmux/config.json</code>
        </li>
      </ul>

      <h3>Overlay System</h3>
      <p>
        When you create a new workspace, schmux can automatically copy local-only files — like{' '}
        <code>.env</code>, API keys, or agent configs — so you don't have to set up secrets for each
        workspace manually.
      </p>

      <h4>Setup</h4>
      <ol className={styles.numberedList}>
        <li>
          Go to the <Link to="/overlays">Overlays</Link> page in the dashboard
        </li>
        <li>
          Click <strong>"+ Add files"</strong> — schmux scans your workspace for candidates (
          <code>.env</code>, <code>.envrc</code>, <code>.tool-versions</code>, etc.) and lets you
          pick which ones to share
        </li>
        <li>
          You can also add custom paths for files that don't exist yet (e.g., agent-generated
          configs)
        </li>
        <li>New workspaces get those files automatically</li>
      </ol>

      <h4>Once configured</h4>
      <ul className={styles.tipsList}>
        <li>
          <strong>Auto-propagation:</strong> If you update an overlay file in one workspace, the
          change propagates to all sibling workspaces for the same repo
        </li>
        <li>
          <strong>Conflict resolution:</strong> If two workspaces change the same overlay file
          differently, schmux uses an LLM to merge the changes
        </li>
        <li>
          <strong>Activity feed:</strong> The Activity tab on the{' '}
          <Link to="/overlays">Overlays page</Link> shows a real-time feed of what synced where
        </li>
      </ul>
    </div>
  );
}
