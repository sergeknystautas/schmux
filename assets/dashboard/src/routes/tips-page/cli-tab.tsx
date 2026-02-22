import styles from '../../styles/tips.module.css';

export function CliTab() {
  return (
    <div>
      <h2>CLI Commands</h2>
      <p>
        The CLI is for speed and scripting — quick commands from the terminal with composable
        operations. Use the web dashboard for observability and real-time monitoring.
      </p>

      <h3>Daemon Management</h3>
      <div className={styles.cmdBlock}>
        <code>
          schmux start # Start daemon in background
          <br />
          schmux stop # Stop daemon
          <br />
          schmux status # Show status and dashboard URL
          <br />
          schmux daemon-run # Run daemon in foreground (debug)
        </code>
      </div>

      <h3>Spawn Sessions</h3>
      <p>
        The <code>schmux spawn</code> command creates new sessions. Workspace is resolved in this
        order:
      </p>
      <ol className={styles.numberedList}>
        <li>
          <strong>
            Explicit <code>-w</code> flag:
          </strong>{' '}
          Use that specific workspace path
        </li>
        <li>
          <strong>
            Explicit <code>-r</code> flag:
          </strong>{' '}
          Create/find workspace for that repo
        </li>
        <li>
          <strong>Neither flag:</strong> Auto-detect if current directory is in a workspace
        </li>
      </ol>

      <div className={styles.cmdBlock}>
        <code>
          # Spawn in current workspace (auto-detected)
          <br />
          # This works when you're cd'd into a workspace directory
          <br />
          schmux spawn -t claude -p "do a code review"
          <br />
          <br />
          # Spawn in specific workspace by ID
          <br />
          schmux spawn -w ~/schmux-workspaces/myproject-001 -t claude -p "do a code review"
          <br />
          <br />
          # Spawn in new workspace (creates new workspace from repo)
          <br />
          schmux spawn -r myproject -t claude -p "implement feature X"
          <br />
          <br />
          # Spawn on specific branch (creates new workspace if needed)
          <br />
          schmux spawn -r myproject -b feature-x -t claude -p "implement feature X"
        </code>
      </div>

      <h3>Session Management</h3>
      <div className={styles.cmdBlock}>
        <code>
          schmux list [--json] # List all sessions
          <br />
          schmux attach &lt;session-id&gt; # Attach to a session via tmux
          <br />
          schmux dispose &lt;session-id&gt; # Dispose a session
        </code>
      </div>

      <h3>When to Use CLI vs Web</h3>
      <ul className={styles.tipsList}>
        <li>
          <strong>Use CLI when:</strong> Already in terminal, quick one-off operations,
          scripting/automation, need JSON output
        </li>
        <li>
          <strong>Use web dashboard when:</strong> Monitoring many sessions, real-time terminal
          output, comparing across agents, visual interaction
        </li>
      </ul>
    </div>
  );
}
