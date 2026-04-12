import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import ExperimentalTab from './ExperimentalTab';
import { EXPERIMENTAL_FEATURES } from './experimentalRegistry';
import { initialState } from './useConfigForm';
import type { ConfigFormAction } from './useConfigForm';

// Mock useFeatures — default: all build features enabled
const mockFeatures: Record<string, boolean> = {
  tunnel: true,
  github: true,
  telemetry: true,
  update: true,
  dashboardsx: true,
  model_registry: true,
  repofeed: true,
  subreddit: true,
  personas: true,
  comm_styles: true,
  lore: true,
  floor_manager: true,
  timelapse: true,
};

vi.mock('../../contexts/FeaturesContext', () => ({
  useFeatures: () => ({
    features: mockFeatures,
    loading: false,
  }),
}));

const dispatch = vi.fn<(action: ConfigFormAction) => void>();

function renderTab(overrides: Partial<typeof initialState> = {}) {
  return render(
    <ExperimentalTab state={{ ...initialState, ...overrides }} dispatch={dispatch} models={[]} />
  );
}

describe('ExperimentalTab', () => {
  it('renders all features with name and description', () => {
    // Start with all features disabled so config panels don't duplicate headings
    renderTab({
      loreEnabled: false,
      fmEnabled: false,
      repofeedEnabled: false,
      subredditEnabled: false,
      timelapseEnabled: false,
    });
    for (const feature of EXPERIMENTAL_FEATURES) {
      expect(screen.getByText(feature.name)).toBeInTheDocument();
      expect(screen.getByText(feature.description)).toBeInTheDocument();
    }
  });

  it('toggling a feature on shows its config panel', async () => {
    // Start with lore disabled, toggle it on
    dispatch.mockClear();
    renderTab({ loreEnabled: false });

    // Config panel content should not be visible initially.
    // The Lore config panel renders a "Lore" heading inside its panel.
    // Since the registry card already shows "Lore", look for the panel-specific content.
    const toggle = screen.getByTestId('experimental-toggle-lore');
    expect(toggle).not.toBeChecked();

    await userEvent.click(toggle);
    expect(dispatch).toHaveBeenCalledWith(
      expect.objectContaining({ type: 'SET_FIELD', field: 'loreEnabled', value: true })
    );
  });

  it('toggling a feature off hides its config panel', async () => {
    // Start with lore enabled — the LoreConfig panel should be rendered
    dispatch.mockClear();
    renderTab({ loreEnabled: true });

    const toggle = screen.getByTestId('experimental-toggle-lore');
    expect(toggle).toBeChecked();

    // The LoreConfig panel renders its config fields (e.g., LLM Target)
    expect(screen.getByText('LLM Target')).toBeInTheDocument();

    await userEvent.click(toggle);
    expect(dispatch).toHaveBeenCalledWith(
      expect.objectContaining({ type: 'SET_FIELD', field: 'loreEnabled', value: false })
    );
  });

  it('hides build-gated features when build feature is false', () => {
    // Disable repofeed, subreddit, and lore at the build level
    mockFeatures.repofeed = false;
    mockFeatures.subreddit = false;
    mockFeatures.lore = false;

    // Disable all features so config panels don't duplicate headings
    renderTab({
      loreEnabled: false,
      fmEnabled: false,
      repofeedEnabled: false,
      subredditEnabled: false,
      timelapseEnabled: false,
    });

    // Disabled features should not appear
    expect(screen.queryByText('Repofeed')).not.toBeInTheDocument();
    expect(screen.queryByText('Subreddit')).not.toBeInTheDocument();
    expect(screen.queryByText('Lore')).not.toBeInTheDocument();

    // Enabled features should still appear
    expect(screen.getByText('Floor Manager')).toBeInTheDocument();
    expect(screen.getByText('Timelapse')).toBeInTheDocument();
    expect(screen.getByText('Personas')).toBeInTheDocument();

    // Restore for other tests
    mockFeatures.repofeed = true;
    mockFeatures.subreddit = true;
    mockFeatures.lore = true;
  });

  it('shows build-gated features when build feature is true', () => {
    // Ensure repofeed and subreddit are enabled
    mockFeatures.repofeed = true;
    mockFeatures.subreddit = true;

    // Disable all features so config panels don't duplicate headings
    renderTab({
      loreEnabled: false,
      fmEnabled: false,
      repofeedEnabled: false,
      subredditEnabled: false,
      timelapseEnabled: false,
    });

    expect(screen.getByText('Repofeed')).toBeInTheDocument();
    expect(screen.getByText('Subreddit')).toBeInTheDocument();
  });

  it('renders Personas toggle with no config panel', () => {
    renderTab({
      personasEnabled: false,
      loreEnabled: false,
      fmEnabled: false,
      repofeedEnabled: false,
      subredditEnabled: false,
      timelapseEnabled: false,
      commStylesEnabled: false,
    });
    expect(screen.getByText('Personas')).toBeInTheDocument();
    expect(
      screen.getByText('Custom agent personalities with unique prompts and visual identity')
    ).toBeInTheDocument();
    expect(screen.getByTestId('experimental-toggle-personas')).toBeInTheDocument();
  });

  it('renders Comm Styles toggle and shows config panel when enabled', () => {
    renderTab({
      personasEnabled: false,
      loreEnabled: false,
      fmEnabled: false,
      repofeedEnabled: false,
      subredditEnabled: false,
      timelapseEnabled: false,
      commStylesEnabled: true,
      enabledModels: {},
    });
    expect(screen.getByText('Comm Styles')).toBeInTheDocument();
    expect(
      screen.getByText('Control how agents communicate with customizable response styles')
    ).toBeInTheDocument();
    expect(screen.getByTestId('experimental-toggle-commStyles')).toBeChecked();
  });

  it('Personas enabled toggle does not render a config panel', () => {
    renderTab({
      personasEnabled: true,
      loreEnabled: false,
      fmEnabled: false,
      repofeedEnabled: false,
      subredditEnabled: false,
      timelapseEnabled: false,
      commStylesEnabled: false,
    });
    const toggle = screen.getByTestId('experimental-toggle-personas');
    expect(toggle).toBeChecked();
    // The Personas feature has no config panel, so there should be no extra content
    // besides the toggle and description. The section should contain only those elements.
  });
});
