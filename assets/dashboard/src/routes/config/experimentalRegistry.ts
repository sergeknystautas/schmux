import type { ConfigFormState } from './useConfigForm';
import type { ConfigPanelProps } from './ConfigPanelProps';
import AutolearnConfig from './AutolearnConfig';
import FloorManagerConfig from './FloorManagerConfig';
import RepofeedConfig from './RepofeedConfig';
import SubredditConfig from './SubredditConfig';
import TimelapseConfig from './TimelapseConfig';
import CommStylesConfig from './CommStylesConfig';

type ExperimentalFeature = {
  id: string;
  name: string;
  description: string;
  enabledKey: keyof ConfigFormState;
  configPanel: React.ComponentType<ConfigPanelProps> | null;
  /** Key in the Features object; if set, the card is hidden when the build feature is false. */
  buildFeatureKey?: string;
};

export const EXPERIMENTAL_FEATURES: ExperimentalFeature[] = [
  {
    id: 'personas',
    name: 'Personas',
    description: 'Custom agent personalities with unique prompts and visual identity',
    enabledKey: 'personasEnabled',
    configPanel: null,
    buildFeatureKey: 'personas',
  },
  {
    id: 'commStyles',
    name: 'Comm Styles',
    description: 'Control how agents communicate with customizable response styles',
    enabledKey: 'commStylesEnabled',
    configPanel: CommStylesConfig,
    buildFeatureKey: 'comm_styles',
  },
  {
    id: 'autolearn',
    name: 'Autolearn',
    description: 'Learns from agent friction and usage patterns — proposes rules and skills',
    enabledKey: 'autolearnEnabled',
    configPanel: AutolearnConfig,
    buildFeatureKey: 'autolearn',
  },
  {
    id: 'floorManager',
    name: 'Floor Manager',
    description: 'An overseer agent \u2014 one conversation to direct all your sessions',
    enabledKey: 'fmEnabled',
    configPanel: FloorManagerConfig,
    buildFeatureKey: 'floor_manager',
  },
  {
    id: 'repofeed',
    name: 'Repofeed',
    description: 'See what teammates are working on right now — live cross-developer activity feed',
    enabledKey: 'repofeedEnabled',
    configPanel: RepofeedConfig,
    buildFeatureKey: 'repofeed',
  },
  {
    id: 'subreddit',
    name: 'Subreddit',
    description: 'LLM-generated digest of landed commits — a per-repo changelog on the home page',
    enabledKey: 'subredditEnabled',
    configPanel: SubredditConfig,
    buildFeatureKey: 'subreddit',
  },
  {
    id: 'timelapse',
    name: 'Timelapse',
    description: 'Terminal recording and playback',
    enabledKey: 'timelapseEnabled',
    configPanel: TimelapseConfig,
    buildFeatureKey: 'timelapse',
  },
  {
    id: 'backburner',
    name: 'Backburner',
    description: 'Dim and sort workspaces you want to set aside',
    enabledKey: 'backburnerEnabled',
    configPanel: null,
  },
];
