import type { ConfigFormState } from './useConfigForm';
import type { ConfigPanelProps } from './ConfigPanelProps';
import LoreConfig from './LoreConfig';
import FloorManagerConfig from './FloorManagerConfig';
import RepofeedConfig from './RepofeedConfig';
import SubredditConfig from './SubredditConfig';
import TimelapseConfig from './TimelapseConfig';

export type ExperimentalFeature = {
  id: string;
  name: string;
  description: string;
  enabledKey: keyof ConfigFormState;
  configPanel: React.ComponentType<ConfigPanelProps>;
  /** Key in the Features object; if set, the card is hidden when the build feature is false. */
  buildFeatureKey?: string;
};

export const EXPERIMENTAL_FEATURES: ExperimentalFeature[] = [
  {
    id: 'lore',
    name: 'Lore',
    description: 'Learns from agent mistakes and suggests instruction updates',
    enabledKey: 'loreEnabled',
    configPanel: LoreConfig,
  },
  {
    id: 'floorManager',
    name: 'Floor Manager',
    description: 'An overseer agent \u2014 one conversation to direct all your sessions',
    enabledKey: 'fmEnabled',
    configPanel: FloorManagerConfig,
  },
  {
    id: 'repofeed',
    name: 'Repofeed',
    description: 'Live feed of repository activity',
    enabledKey: 'repofeedEnabled',
    configPanel: RepofeedConfig,
    buildFeatureKey: 'repofeed',
  },
  {
    id: 'subreddit',
    name: 'Subreddit',
    description: 'Reddit-style discussion threads',
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
  },
];
