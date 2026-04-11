import { useState } from 'react';
import styles from '../../styles/tips.module.css';
import { TmuxTab } from './tmux-tab';
import { CliTab } from './cli-tab';
import { WorkflowTab } from './workflow-tab';
import { QualityOfLifeTab } from './quality-of-life-tab';
import { ShortcutsTab } from './shortcuts-tab';
import { PowerToolsTab } from './power-tools-tab';
import { PromptsTab } from './prompts-tab';

const TABS = [
  'tmux',
  'CLI',
  'Workflow',
  'Quality of Life',
  'Shortcuts',
  'Power Tools',
  'Prompts',
] as const;
const TOTAL_TABS = TABS.length;

const TAB_COMPONENTS = [
  TmuxTab,
  CliTab,
  WorkflowTab,
  QualityOfLifeTab,
  ShortcutsTab,
  PowerToolsTab,
  PromptsTab,
] as const;

export default function TipsPage() {
  const [currentTab, setCurrentTab] = useState(1);

  const ActiveTab = TAB_COMPONENTS[currentTab - 1];

  return (
    <>
      <div className="config-sticky-header">
        <div className="config-sticky-header__title-row">
          <h1 className="config-sticky-header__title">Tips</h1>
        </div>
        <div className="config-tabs">
          {Array.from({ length: TOTAL_TABS }, (_, i) => i + 1).map((tabNum) => {
            const isCurrent = tabNum === currentTab;
            const tabLabel = TABS[tabNum - 1];

            return (
              <button
                key={tabNum}
                className={`config-tab ${isCurrent ? 'config-tab--active' : ''}`}
                onClick={() => setCurrentTab(tabNum)}
              >
                {tabLabel}
              </button>
            );
          })}
        </div>
      </div>

      <div className={styles.tipsContainer}>
        <div className={styles.tipsContent}>
          <ActiveTab />
        </div>
      </div>
    </>
  );
}
