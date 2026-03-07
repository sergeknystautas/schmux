import Hero from './sections/Hero';
import Sessions from './sections/Sessions';
import Spawn from './sections/Spawn';
import Overlays from './sections/Overlays';
import ConflictResolution from './sections/ConflictResolution';
import Personas from './sections/Personas';
import CodeReview from './sections/CodeReview';
import Digest from './sections/Digest';
import FloorManager from './sections/FloorManager';
import RemoteAccess from './sections/RemoteAccess';
import RemoteHosts from './sections/RemoteHosts';
import ModelCatalog from './sections/ModelCatalog';
import WhyItMatters from './sections/WhyItMatters';
import Close from './sections/Close';

export default function App() {
  return (
    <div className="website">
      <Hero />
      <Sessions />
      <Spawn />
      <Overlays />
      <ConflictResolution />
      <Personas />
      <CodeReview />
      <Digest />
      <FloorManager />
      <RemoteAccess />
      <RemoteHosts />
      <ModelCatalog />
      <WhyItMatters />
      <Close />
    </div>
  );
}
