import { createRoot } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import '@dashboard/styles/global.css';
import DemoShell, { createScenarioSetup } from './DemoShell';

// Parse scenario from hash: #/workspaces → "workspaces", #/spawn → "spawn"
function getScenarioFromHash(): string {
  const hash = window.location.hash.replace('#/', '').split('/')[0];
  return hash || 'workspaces';
}

const root = createRoot(document.getElementById('root')!);

function render() {
  const scenarioId = getScenarioFromHash();
  const setup = createScenarioSetup(scenarioId);

  root.render(
    <MemoryRouter key={scenarioId} initialEntries={['/']}>
      <DemoShell setup={setup} />
    </MemoryRouter>
  );
}

// Re-render when hash changes (scenario switching via banner links)
window.addEventListener('hashchange', render);
render();
