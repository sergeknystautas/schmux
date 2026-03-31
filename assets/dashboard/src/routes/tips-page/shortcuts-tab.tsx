import styles from '../../styles/tips.module.css';

export function ShortcutsTab() {
  return (
    <div>
      <h2>Dashboard Keyboard Shortcuts</h2>
      <p>
        The dashboard has a keyboard mode activated by pressing{' '}
        <span className={styles.keyCombo}>⌘/</span> (or{' '}
        <span className={styles.keyCombo}>Ctrl+/</span> on Linux/Windows). Once activated, press a
        key to execute the action. Press <span className={styles.keyCombo}>Esc</span> to cancel.
      </p>

      <h3>Global</h3>
      <p>Available from any page in the dashboard.</p>
      <table className={styles.modelTable}>
        <thead>
          <tr>
            <th>Key</th>
            <th>Action</th>
          </tr>
        </thead>
        <tbody>
          <tr>
            <td>
              <span className={styles.keyCombo}>N</span>
            </td>
            <td>Spawn new session (context-aware — uses current workspace if inside one)</td>
          </tr>
          <tr>
            <td>
              <span className={styles.keyCombo}>Shift+N</span>
            </td>
            <td>Spawn new session (always opens general spawn wizard)</td>
          </tr>
          <tr>
            <td>
              <span className={styles.keyCombo}>H</span>
            </td>
            <td>Go to home page</td>
          </tr>
          <tr>
            <td>
              <span className={styles.keyCombo}>?</span>
            </td>
            <td>Show keyboard shortcuts help modal</td>
          </tr>
        </tbody>
      </table>

      <h3>Workspace</h3>
      <p>Available when you are viewing a workspace or a session within a workspace.</p>
      <table className={styles.modelTable}>
        <thead>
          <tr>
            <th>Key</th>
            <th>Action</th>
          </tr>
        </thead>
        <tbody>
          <tr>
            <td>
              <span className={styles.keyCombo}>D</span>
            </td>
            <td>Go to diff page for the current workspace</td>
          </tr>
          <tr>
            <td>
              <span className={styles.keyCombo}>G</span>
            </td>
            <td>Go to git graph for the current workspace</td>
          </tr>
          <tr>
            <td>
              <span className={styles.keyCombo}>V</span>
            </td>
            <td>Open the current workspace in VS Code</td>
          </tr>
          <tr>
            <td>
              <span className={styles.keyCombo}>W</span>
            </td>
            <td>Dispose active session or close active tab</td>
          </tr>
          <tr>
            <td>
              <span className={styles.keyCombo}>Shift+W</span>
            </td>
            <td>Dispose workspace (requires confirmation)</td>
          </tr>
        </tbody>
      </table>

      <h3>Session</h3>
      <p>Available when you are viewing a specific session.</p>
      <table className={styles.modelTable}>
        <thead>
          <tr>
            <th>Key</th>
            <th>Action</th>
          </tr>
        </thead>
        <tbody>
          <tr>
            <td>
              <span className={styles.keyCombo}>↓</span>
            </td>
            <td>Resume / scroll to bottom</td>
          </tr>
        </tbody>
      </table>

      <h3>Direct Shortcuts</h3>
      <p>These shortcuts work without entering keyboard mode.</p>
      <table className={styles.modelTable}>
        <thead>
          <tr>
            <th>Key</th>
            <th>Action</th>
          </tr>
        </thead>
        <tbody>
          <tr>
            <td>
              <span className={styles.keyCombo}>⌘/ / Ctrl+/</span>
            </td>
            <td>Toggle keyboard mode</td>
          </tr>
          <tr>
            <td>
              <span className={styles.keyCombo}>⌘← / Ctrl+←</span>
            </td>
            <td>Previous session in workspace</td>
          </tr>
          <tr>
            <td>
              <span className={styles.keyCombo}>⌘→ / Ctrl+→</span>
            </td>
            <td>Next session in workspace</td>
          </tr>
          <tr>
            <td>
              <span className={styles.keyCombo}>⌘↑ / Ctrl+↑</span>
            </td>
            <td>Previous workspace</td>
          </tr>
          <tr>
            <td>
              <span className={styles.keyCombo}>⌘↓ / Ctrl+↓</span>
            </td>
            <td>Next workspace</td>
          </tr>
          <tr>
            <td>
              <span className={styles.keyCombo}>⌘Enter / Ctrl+Enter</span>
            </td>
            <td>Submit spawn form (on spawn page)</td>
          </tr>
        </tbody>
      </table>
    </div>
  );
}
