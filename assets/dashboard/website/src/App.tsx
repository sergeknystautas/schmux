import Hero from './sections/Hero';
import Sessions from './sections/Sessions';
import Spawn from './sections/Spawn';
import Personas from './sections/Personas';
import FloorManager from './sections/FloorManager';
import WhyItMatters from './sections/WhyItMatters';
import Close from './sections/Close';

export default function App() {
  return (
    <div className="website">
      <Hero />
      <Sessions />
      <Spawn />
      <Personas />
      <FloorManager />
      <WhyItMatters />
      <Close />
    </div>
  );
}
