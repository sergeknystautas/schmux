import type { ConfigFormState, ConfigFormAction } from './useConfigForm';
import type { Model } from '../../lib/types';

export type ConfigPanelProps = {
  state: ConfigFormState;
  dispatch: React.Dispatch<ConfigFormAction>;
  models: Model[];
};
