import { useCuration } from '../contexts/CurationContext';
import CuratorTerminal from './CuratorTerminal';

export default function CurationStatus() {
  const { activeCurations } = useCuration();
  const entries = Object.entries(activeCurations);

  if (entries.length === 0) return null;

  return (
    <div className="curation-status">
      <span className="nav-section-title">Lore Curation</span>
      {entries.map(([repo, state]) => (
        <div key={repo}>
          <div className="curation-status__item">
            <span className="curation-status__spinner" />
            <span className="curation-status__repo">{repo}</span>
            <span className="curation-status__message">{state.message}</span>
            <span className="curation-status__elapsed">{state.elapsed}s</span>
          </div>
          {state.events && <CuratorTerminal events={state.events} />}
        </div>
      ))}
    </div>
  );
}
