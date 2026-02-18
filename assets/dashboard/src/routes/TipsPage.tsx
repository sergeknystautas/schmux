import { useState } from 'react';
import { Link } from 'react-router-dom';
import styles from '../styles/tips.module.css';
import { useRequireConfig } from '../contexts/ConfigContext';

const TABS = ['tmux', 'CLI', 'Workflow', 'Quality of Life', 'Shortcuts', 'Power Tools'] as const;
const TOTAL_TABS = TABS.length;

export default function TipsPage() {
  useRequireConfig();
  const [currentTab, setCurrentTab] = useState(1);

  return (
    <>
      <div className="config-sticky-header">
        <div className="config-sticky-header__title-row">
          <h1 className="config-sticky-header__title">Tips</h1>
        </div>
        <div className="config-tabs">
          {Array.from({ length: TOTAL_TABS }, (_, i) => i + 1).map((tabNum) => {
            const isCurrent = tabNum === currentTab;
            const tabLabel = TABS[tabNum - 1];

            return (
              <button
                key={tabNum}
                className={`config-tab ${isCurrent ? 'config-tab--active' : ''}`}
                onClick={() => setCurrentTab(tabNum)}
              >
                {tabLabel}
              </button>
            );
          })}
        </div>
      </div>

      <div className={styles.tipsContainer}>
        <div className={styles.tipsContent}>
          {currentTab === 1 && (
            <div>
              <h2>Why tmux?</h2>
              <p>
                schmux runs every agent session inside tmux, a terminal multiplexer. Each session
                gets its own isolated tmux session that you can attach to at any time.
              </p>

              <h3>Why not other approaches?</h3>
              <ul className={styles.tipsList}>
                <li>
                  <strong>Not Claude Code plugins/subagents:</strong> Claude Code's approach ties
                  you to their ecosystem. schmux works with any tool that runs in a terminal —
                  Claude, Codex, Gemini, or custom scripts. You're not locked into one vendor.
                </li>
                <li>
                  <strong>Not Docker:</strong> Containers add overhead and complexity. schmux uses
                  your actual filesystem and tools — no network setup, no volume mounting, no
                  container orchestration. Just directories on your machine.
                </li>
                <li>
                  <strong>tmux gives you persistence:</strong> Sessions survive disconnects. You can
                  close your laptop, come back tomorrow, and the agent is still there. You can
                  scroll back through history, attach from different terminals, and never lose
                  context.
                </li>
                <li>
                  <strong>tmux is industry standard:</strong> It's been battle-tested for decades.
                  The keybindings are known by millions of developers. No need to learn a custom UI.
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
                  <strong>Create new window:</strong>{' '}
                  <span className={styles.keyCombo}>Ctrl+b</span> then{' '}
                  <span className={styles.keyCombo}>c</span>
                </li>
                <li>
                  <strong>Switch windows:</strong> <span className={styles.keyCombo}>Ctrl+b</span>{' '}
                  then <span className={styles.keyCombo}>0</span>-
                  <span className={styles.keyCombo}>9</span>
                </li>
                <li>
                  <strong>List windows:</strong> <span className={styles.keyCombo}>Ctrl+b</span>{' '}
                  then <span className={styles.keyCombo}>w</span>
                </li>
                <li>
                  <strong>Rename window:</strong> <span className={styles.keyCombo}>Ctrl+b</span>{' '}
                  then <span className={styles.keyCombo}>,</span>
                </li>
                <li>
                  <strong>Search for text:</strong> <span className={styles.keyCombo}>Ctrl+b</span>{' '}
                  then <span className={styles.keyCombo}>[</span>, then{' '}
                  <span className={styles.keyCombo}>Ctrl+s</span>
                </li>
                <li>
                  <strong>Split pane horizontal:</strong>{' '}
                  <span className={styles.keyCombo}>Ctrl+b</span> then{' '}
                  <span className={styles.keyCombo}>%</span>
                </li>
                <li>
                  <strong>Split pane vertical:</strong>{' '}
                  <span className={styles.keyCombo}>Ctrl+b</span> then{' '}
                  <span className={styles.keyCombo}>"</span>
                </li>
                <li>
                  <strong>Navigate panes:</strong> <span className={styles.keyCombo}>Ctrl+b</span>{' '}
                  then <span className={styles.keyCombo}>o</span>
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
                  Pro tip: Click the "copy attach command" button in the session detail page to get
                  the exact attach command for any session.
                </em>
              </p>
            </div>
          )}

          {currentTab === 2 && (
            <div>
              <h2>CLI Commands</h2>
              <p>
                The CLI is for speed and scripting — quick commands from the terminal with
                composable operations. Use the web dashboard for observability and real-time
                monitoring.
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
                The <code>schmux spawn</code> command creates new sessions. Workspace is resolved in
                this order:
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
                  <strong>Use web dashboard when:</strong> Monitoring many sessions, real-time
                  terminal output, comparing across agents, visual interaction
                </li>
              </ul>
            </div>
          )}

          {currentTab === 3 && (
            <div>
              <h2>Workflow Guide</h2>
              <p>
                schmux is designed for running multiple agents in parallel on the same codebase.
                Each agent has strengths — use them together to get better results faster.
              </p>

              <h3>Multi-Agent Strategy</h3>
              <ul className={styles.tipsList}>
                <li>
                  <strong>Parallel reviews:</strong> Spawn different agents (Claude, Kimi, Codex) on
                  the same branch to get diverse perspectives
                </li>
                <li>
                  <strong>Specialized tasks:</strong> Use fast models for quick edits, powerful
                  models for complex refactoring
                </li>
                <li>
                  <strong>Comparison:</strong> Use the diff viewer to compare approaches across
                  workspaces
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
                      <code>claude-opus</code>
                    </td>
                    <td>Complex reasoning, architecture, large refactors</td>
                  </tr>
                  <tr>
                    <td>
                      <code>claude-sonnet</code>
                    </td>
                    <td>Balanced speed/quality, feature work, debugging</td>
                  </tr>
                  <tr>
                    <td>
                      <code>claude-haiku</code>
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
                  <code>
                    schmux spawn -r myrepo -b feature-x -t claude-haiku -p "create feature X"
                  </code>
                </li>
                <li>
                  <strong>Let the agent work:</strong> Monitor via dashboard or attach with tmux to
                  watch in real-time
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
                  <strong>Commit and sync:</strong> When satisfied, commit changes and sync back to
                  main
                </li>
              </ol>

              <h3>Branch Strategy</h3>
              <p>
                Each workspace uses a specific branch. This isolates work and makes it easy to
                compare approaches.
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
                schmux is built around a linear git workflow. Here's the expected lifecycle for a
                feature:
              </p>
              <ol className={styles.numberedList}>
                <li>
                  <strong>Branch:</strong> Every <code>schmux spawn</code> creates an isolated
                  workspace with its own feature branch. Agents work on their branch without
                  stepping on each other.
                </li>
                <li>
                  <strong>Work:</strong> Let agents make changes. Monitor via the dashboard, attach
                  via tmux when you need to intervene.
                </li>
                <li>
                  <strong>Stay current:</strong> Use "Sync from main" in the{' '}
                  <Link to="/">git graph page</Link> to pull in all new commits from main into your
                  branch. If there are conflicts, schmux uses an LLM to resolve them automatically.
                </li>
                <li>
                  <strong>Ship:</strong> When ready, use "Sync to main" to fast-forward main to your
                  branch. Or push your branch and create a PR externally.
                </li>
              </ol>
              <p>
                <em>
                  schmux avoids merge commits. Sync-from-main cherry-picks, sync-to-main
                  fast-forwards. Your history stays linear.
                </em>
              </p>
            </div>
          )}

          {currentTab === 4 && (
            <div>
              <h2>Quality of Life Features</h2>
              <p>
                schmux is highly focused on developer ergonomics. We've built numerous features to
                reduce friction and keep you in the flow.
              </p>

              <h3>NudgeNik</h3>
              <p>
                NudgeNik reads agent output and classifies their state so you can focus on what
                needs attention:
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
                Status appears on session tabs throughout the dashboard, helping you triage where to
                focus.
              </p>

              <h3>Quick Launch Presets</h3>
              <p>Save common combinations of target + prompt for one-click execution:</p>

              <ul className={styles.tipsList}>
                <li>
                  <strong>Define presets:</strong> Add to <code>~/.schmux/config.json</code>{' '}
                  (global) or workspace <code>.schmux/config.json</code> (repo-specific)
                </li>
                <li>
                  <strong>Access anywhere:</strong> Appears in spawn dropdown and spawn wizard
                </li>
                <li>
                  <strong>Perfect for repetitive tasks:</strong> "Run tests", "Commit changes",
                  "Review PR", "Fix failing tests"
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
                    <td>
                      Side-by-side diff in dashboard or external tools (VS Code, Kaleidoscope)
                    </td>
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
                  <strong>Copy attach command:</strong> Session detail page has a button to copy the
                  exact tmux attach command
                </li>
                <li>
                  <strong>Bulk spawn:</strong> Spawn multiple agents at once from the spawn wizard
                </li>
                <li>
                  <strong>Nicknames:</strong> Give sessions nicknames to easily identify them (e.g.,
                  "reviewer", "fixer")
                </li>
                <li>
                  <strong>JSON output:</strong> Use <code>--json</code> flag with CLI commands for
                  scripting
                </li>
                <li>
                  <strong>Workspace config:</strong> Add repo-specific quick launch presets in{' '}
                  <code>&lt;workspace&gt;/.schmux/config.json</code>
                </li>
              </ul>

              <h3>Overlay System</h3>
              <p>
                When you create a new workspace, schmux can automatically copy local-only files —
                like <code>.env</code>, API keys, or agent configs — so you don't have to set up
                secrets for each workspace manually.
              </p>

              <h4>Setup</h4>
              <ol className={styles.numberedList}>
                <li>
                  Go to the <Link to="/overlays">Overlays</Link> page in the dashboard
                </li>
                <li>
                  Click <strong>"+ Add files"</strong> — schmux scans your workspace for candidates
                  (<code>.env</code>, <code>.envrc</code>, <code>.tool-versions</code>, etc.) and
                  lets you pick which ones to share
                </li>
                <li>
                  You can also add custom paths for files that don't exist yet (e.g.,
                  agent-generated configs)
                </li>
                <li>New workspaces get those files automatically</li>
              </ol>

              <h4>Once configured</h4>
              <ul className={styles.tipsList}>
                <li>
                  <strong>Auto-propagation:</strong> If you update an overlay file in one workspace,
                  the change propagates to all sibling workspaces for the same repo
                </li>
                <li>
                  <strong>Conflict resolution:</strong> If two workspaces change the same overlay
                  file differently, schmux uses an LLM to merge the changes
                </li>
                <li>
                  <strong>Activity feed:</strong> The Activity tab on the{' '}
                  <Link to="/overlays">Overlays page</Link> shows a real-time feed of what synced
                  where
                </li>
              </ul>
            </div>
          )}

          {currentTab === 5 && (
            <div>
              <h2>Dashboard Keyboard Shortcuts</h2>
              <p>
                The dashboard has a keyboard mode activated by pressing{' '}
                <span className={styles.keyCombo}>⌘K</span> (or{' '}
                <span className={styles.keyCombo}>Ctrl+K</span> on Linux/Windows). Once activated,
                press a key to execute the action. Press{' '}
                <span className={styles.keyCombo}>Esc</span> to cancel.
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
                    <td>
                      Spawn new session (context-aware — uses current workspace if inside one)
                    </td>
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
                      <span className={styles.keyCombo}>W</span>
                    </td>
                    <td>Dispose session (requires confirmation)</td>
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
                      <span className={styles.keyCombo}>⌘K / Ctrl+K</span>
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
          )}

          {currentTab === 6 && (
            <div>
              <h2>Power Tools</h2>
              <p>
                Advanced features for power users. These are harder to discover but can
                significantly improve your workflow.
              </p>

              <h3>Remote Hosts</h3>
              <p>
                Run agents on remote machines. schmux is transport-agnostic — you define the
                commands to connect, provision, and reconnect. SSH is the most common choice, but
                any command that gives you a shell works.
              </p>

              <h4>How it works</h4>
              <ol className={styles.numberedList}>
                <li>
                  Configure a remote "flavor" in{' '}
                  <Link to="/settings/remote">Settings &gt; Remote</Link> — define your connect
                  command, provision command, and reconnect command
                </li>
                <li>
                  When spawning a session, select your remote flavor as the target environment
                </li>
                <li>
                  schmux runs your commands to get a working environment on the remote host, then
                  streams the terminal back to your dashboard
                </li>
              </ol>

              <h4>What you get</h4>
              <ul className={styles.tipsList}>
                <li>
                  <strong>Live terminal streaming:</strong> Remote sessions stream to your
                  dashboard, same as local
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
                  <strong>Curation:</strong> When sessions or workspaces are disposed, an LLM
                  curator reviews the raw lore and extracts useful learnings
                </li>
                <li>
                  <strong>Proposals:</strong> Proposals appear for you to review, apply, or dismiss
                </li>
                <li>
                  <strong>Applied lore:</strong> Gets committed to your repo's instruction files
                  (like <code>CLAUDE.md</code>), so future agents benefit
                </li>
              </ul>
              <p>
                <em>
                  Configure in the <Link to="/config">config page</Link> (lore section) — set which
                  LLM curates, when curation triggers, and which instruction files to update.
                </em>
              </p>

              <h3>GitHub PR Review</h3>
              <p>
                schmux can discover open pull requests on your configured repos and help you review
                them with an AI agent.
              </p>

              <h4>How it works</h4>
              <ol className={styles.numberedList}>
                <li>schmux polls GitHub for open PRs on your repos (hourly, up to 5 per repo)</li>
                <li>PRs appear on the home page</li>
                <li>
                  Click a PR to check it out into a new workspace and spawn a review session with
                  full PR context
                </li>
              </ol>
              <p>
                <em>
                  Setup: Run <code>schmux auth github</code> to configure GitHub access, then set a{' '}
                  <code>pr_review.target</code> in your <Link to="/config">config</Link> to define
                  which LLM reviews PRs.
                </em>
              </p>
            </div>
          )}
        </div>
      </div>
    </>
  );
}
