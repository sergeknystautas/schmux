import styles from '../../styles/tips.module.css';

export function PromptsTab() {
  return (
    <div>
      <h2>Prompt Engineering</h2>
      <p>
        When agents misbehave, asking the right questions can get them back on track. These
        techniques help diagnose and fix common issues.
      </p>

      <h3>Brainstorm Before Planning</h3>
      <p>
        Before entering plan mode, use{' '}
        <a href="https://github.com/obra/superpowers" target="_blank" rel="noopener noreferrer">
          superpowers
        </a>{' '}
        as a Claude Code plugin to brainstorm. This helps explore requirements, trade-offs, and
        design alternatives before committing to an implementation path.
      </p>

      <h3>Debugging Agent Behavior</h3>
      <table className={styles.modelTable}>
        <thead>
          <tr>
            <th>When</th>
            <th>Ask</th>
          </tr>
        </thead>
        <tbody>
          <tr>
            <td>
              <strong>Context poison</strong> — agent doing something undesired
            </td>
            <td>"What inspired you to take that approach?"</td>
          </tr>
          <tr>
            <td>
              <strong>Acting without thinking</strong> — no evidence for claims
            </td>
            <td>"Show me the evidence or documentation that this is accurate."</td>
          </tr>
          <tr>
            <td>
              <strong>Changes don't work</strong> — stuck in a loop
            </td>
            <td>"What is confusing you about this?"</td>
          </tr>
        </tbody>
      </table>
    </div>
  );
}
