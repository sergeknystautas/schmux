import React from 'react';
import styles from '../styles/tips.module.css';
import { useRequireConfig } from '../contexts/ConfigContext.jsx';

export default function TipsPage() {
  useRequireConfig();
  return (
    <>
      <div className="page-header">
        <h1 className="page-header__title">Tips</h1>
      </div>

      <div className={styles.tipsSection}>
        <p>Welcome to schmux! Here are some tips to help you get the most out of running AI agents in tmux sessions.</p>

        <h2>tmux</h2>
        <p>Every schmux session runs in its own tmux session. You can attach directly from your terminal to interact with the agent in real-time.</p>

        <h3>Key Shortcuts</h3>
        <p>Once inside a tmux session, use these key combinations (note: tmux uses <span className={styles.keyCombo}>Ctrl+b</span> as its prefix by default):</p>

        <ul className={styles.tipsList}>
          <li><strong>Detach:</strong> <span className={styles.keyCombo}>Ctrl+b</span> then <span className={styles.keyCombo}>d</span></li>
          <li><strong>Scroll:</strong> <span className={styles.keyCombo}>Ctrl+b</span> then <span className={styles.keyCombo}>[</span></li>
          <li><strong>Exit scroll:</strong> <span className={styles.keyCombo}>Esc</span> or <span className={styles.keyCombo}>q</span></li>
          <li><strong>List sessions:</strong> <span className={styles.keyCombo}>Ctrl+b</span> then <span className={styles.keyCombo}>w</span></li>
        </ul>

        <h3>Command Line</h3>
        <p>You can also interact with tmux from your terminal:</p>

        <div className={styles.cmdBlock}>
          <code>
            # List all schmux sessions
            <br />
            tmux ls
            <br />
            <br />
            # Attach to a specific session (replace SESSION_NAME with actual session name)
            <br />
            tmux attach -t SESSION_NAME
          </code>
        </div>

        <p><em>Pro tip: Click the "copy attach command" button in the session detail page to get the exact attach command for any session.</em></p>
      </div>
    </>
  );
}
