import type { ConfigFormState, ConfigFormAction } from './useConfigForm';
import type { TargetOption } from './TargetSelect';

export type ConfigPanelProps = {
  state: ConfigFormState;
  dispatch: React.Dispatch<ConfigFormAction>;
  models: TargetOption[];
};
