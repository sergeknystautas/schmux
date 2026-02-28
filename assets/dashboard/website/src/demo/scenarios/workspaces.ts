import type { TourScenario } from '../tour/types';

export const workspacesScenario: TourScenario = {
  id: 'workspaces',
  title: 'Monitor Running Agents',
  description: 'See how schmux lets you watch multiple AI agents work simultaneously.',
  initialRoute: '/',
  steps: [
    {
      target: '[data-tour="sidebar-workspace-list"]',
      title: 'Your workspaces',
      body: 'The sidebar shows all your active workspaces. Each workspace is a git branch where agents are working.',
      placement: 'right',
      advanceOn: 'next',
    },
    {
      target: '[data-tour="sidebar-session"]',
      title: 'Running sessions',
      body: "Each session is an AI agent working in a workspace. The spinner means it's actively running.",
      placement: 'right',
      advanceOn: 'next',
    },
    {
      target: '[data-tour="sidebar-nudge"]',
      title: 'Needs your input',
      body: 'When an agent stops and waits for input, you see it flagged in the sidebar so you know where to look.',
      placement: 'right',
      advanceOn: 'next',
    },
    {
      target: '[data-tour="sidebar-session"]',
      title: 'Open a session',
      body: 'Click on a session to see its live terminal output.',
      placement: 'right',
      advanceOn: 'click',
      afterStep: () => {
        // Navigation happens via React Router click handler on the session
      },
    },
    {
      target: '[data-tour="terminal-viewport"]',
      title: 'Live terminal',
      body: "Watch the agent work in real time. This is the same terminal output you'd see in tmux.",
      placement: 'left',
      advanceOn: 'next',
      route: '/sessions/demo-sess-1',
    },
    {
      target: '[data-tour="session-detail-sidebar"]',
      title: 'Session details',
      body: 'See the agent type, persona, branch, and attach command. You can always jump into tmux directly.',
      placement: 'left',
      advanceOn: 'next',
      route: '/sessions/demo-sess-1',
      beforeStep: () => {
        // Ensure sidebar is expanded — it may be collapsed via localStorage
        const key = 'schmux:sessionSidebarCollapsed';
        localStorage.setItem(key, JSON.stringify(false));
        window.dispatchEvent(new StorageEvent('storage', { key, newValue: JSON.stringify(false) }));
      },
    },
    {
      target: '[data-tour="session-tabs"]',
      title: 'Switch between agents',
      body: 'Tabs let you quickly flip between agents working in the same workspace. All running in parallel.',
      placement: 'left',
      advanceOn: 'next',
      route: '/sessions/demo-sess-2',
    },
    {
      target: '[data-tour="diff-tab"]',
      title: 'Changed files',
      body: 'See what files each agent has changed. Review diffs right in the dashboard without leaving your browser.',
      placement: 'bottom',
      advanceOn: 'next',
      route: '/diff/demo-ws-1',
    },
    {
      target: '[data-tour="git-tab"]',
      title: 'Commit graph',
      body: 'Visualize the commit history for each workspace. See how far ahead or behind the branch is.',
      placement: 'bottom',
      advanceOn: 'next',
      route: '/git/demo-ws-1',
    },
    {
      target: '[data-tour="vscode-btn"]',
      title: 'Open in VS Code',
      body: "Jump straight into VS Code to edit any workspace. One click and you're in the code.",
      placement: 'bottom',
      advanceOn: 'next',
    },
  ],
};
