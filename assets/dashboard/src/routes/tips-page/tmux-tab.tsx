import styles from '../../styles/tips.module.css';

export function TmuxTab() {
  return (
    <div>
      <h2>Why tmux?</h2>
      <p>
        schmux runs every agent session inside tmux, a terminal multiplexer. Each session gets its
        own isolated tmux session that you can attach to at any time.
      </p>

      <h3>Why not other approaches?</h3>
      <ul className={styles.tipsList}>
        <li>
          <strong>Not Claude Code plugins/subagents:</strong> Claude Code's approach ties you to
          their ecosystem. schmux works with any tool that runs in a terminal — Claude, Codex,
          Gemini, or custom scripts. You're not locked into one vendor.
        </li>
        <li>
          <strong>Not Docker:</strong> Containers add overhead and complexity. schmux uses your
          actual filesystem and tools — no network setup, no volume mounting, no container
          orchestration. Just directories on your machine.
        </li>
        <li>
          <strong>tmux gives you persistence:</strong> Sessions survive disconnects. You can close
          your laptop, come back tomorrow, and the agent is still there. You can scroll back through
          history, attach from different terminals, and never lose context.
        </li>
        <li>
          <strong>tmux is industry standard:</strong> It's been battle-tested for decades. The
          keybindings are known by millions of developers. No need to learn a custom UI.
        </li>
      </ul>

      <h3>Key Shortcuts</h3>
      <p>
        Once inside a tmux session, use these key combinations (tmux uses{' '}
        <span className={styles.keyCombo}>Ctrl+b</span> as its prefix):
      </p>

      <ul className={styles.tipsList}>
        <li>
          <strong>Detach:</strong> <span className={styles.keyCombo}>Ctrl+b</span> then{' '}
          <span className={styles.keyCombo}>d</span>
        </li>
        <li>
          <strong>Scroll:</strong> <span className={styles.keyCombo}>Ctrl+b</span> then{' '}
          <span className={styles.keyCombo}>[</span>
        </li>
        <li>
          <strong>Exit scroll:</strong> <span className={styles.keyCombo}>Esc</span> or{' '}
          <span className={styles.keyCombo}>q</span>
        </li>
        <li>
          <strong>Create new window:</strong> <span className={styles.keyCombo}>Ctrl+b</span> then{' '}
          <span className={styles.keyCombo}>c</span>
        </li>
        <li>
          <strong>Switch windows:</strong> <span className={styles.keyCombo}>Ctrl+b</span> then{' '}
          <span className={styles.keyCombo}>0</span>-<span className={styles.keyCombo}>9</span>
        </li>
        <li>
          <strong>List windows:</strong> <span className={styles.keyCombo}>Ctrl+b</span> then{' '}
          <span className={styles.keyCombo}>w</span>
        </li>
        <li>
          <strong>Rename window:</strong> <span className={styles.keyCombo}>Ctrl+b</span> then{' '}
          <span className={styles.keyCombo}>,</span>
        </li>
        <li>
          <strong>Search for text:</strong> <span className={styles.keyCombo}>Ctrl+b</span> then{' '}
          <span className={styles.keyCombo}>[</span>, then{' '}
          <span className={styles.keyCombo}>Ctrl+s</span>
        </li>
        <li>
          <strong>Split pane horizontal:</strong> <span className={styles.keyCombo}>Ctrl+b</span>{' '}
          then <span className={styles.keyCombo}>%</span>
        </li>
        <li>
          <strong>Split pane vertical:</strong> <span className={styles.keyCombo}>Ctrl+b</span> then{' '}
          <span className={styles.keyCombo}>"</span>
        </li>
        <li>
          <strong>Navigate panes:</strong> <span className={styles.keyCombo}>Ctrl+b</span> then{' '}
          <span className={styles.keyCombo}>o</span>
        </li>
      </ul>

      <h3>Command Line</h3>
      <p>Interact with tmux from your terminal:</p>

      <div className={styles.cmdBlock}>
        <code>
          # List all schmux sessions
          <br />
          tmux ls
          <br />
          <br />
          # Attach to a specific session
          <br />
          tmux attach -t SESSION_NAME
          <br />
          <br />
          # Kill a session
          <br />
          tmux kill-session -t SESSION_NAME
        </code>
      </div>

      <p>
        <em>
          Pro tip: Click the "copy attach command" button in the session detail page to get the
          exact attach command for any session.
        </em>
      </p>
    </div>
  );
}
