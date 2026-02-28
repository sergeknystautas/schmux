import type { TourScenario } from '../tour/types';

/** Base scenario definition. beforeStep/afterStep hooks are wired in DemoShell. */
export const spawnScenario: TourScenario = {
  id: 'spawn',
  title: 'Spawn Your First Agent',
  description: 'Launch an AI coding agent and watch it start working.',
  initialRoute: '/',
  steps: [
    {
      target: '[data-tour="sidebar-add-workspace"]',
      title: 'Add a workspace',
      body: 'Click "Add Workspace" to spawn a new AI agent. This opens the spawn wizard.',
      placement: 'right',
      advanceOn: 'click',
    },
    {
      target: '[data-tour="spawn-form"]',
      title: 'The spawn wizard',
      body: 'Configure your agent here — pick a repo, choose a persona, and describe what you want built.',
      placement: 'left',
      advanceOn: 'next',
      route: '/spawn',
    },
    {
      target: '[data-tour="spawn-repo-select"]',
      title: 'Pick a repository',
      body: 'Select which codebase the agent will work in. schmux creates an isolated git worktree for each workspace.',
      placement: 'bottom',
      advanceOn: 'next',
    },
    {
      target: '[data-tour="spawn-persona-select"]',
      title: 'Choose a persona',
      body: 'Personas shape how the agent approaches work — a "Backend Engineer" focuses on APIs, a "QA Engineer" on tests.',
      placement: 'bottom',
      advanceOn: 'next',
    },
    {
      target: '[data-tour="spawn-submit"]',
      title: 'Launch the agent',
      body: 'Hit Engage to spawn the agent. It starts working immediately in its own tmux session.',
      placement: 'top',
      advanceOn: 'click',
      // afterStep: wired in DemoShell to update workspaces with new session
    },
    {
      target: '[data-tour="terminal-viewport"]',
      title: 'Watch it work',
      body: 'Your agent is now running! Watch it analyze the codebase, write code, and run tests — all autonomously.',
      placement: 'left',
      advanceOn: 'next',
      // route + beforeStep: wired in DemoShell to navigate to new session
    },
  ],
};
