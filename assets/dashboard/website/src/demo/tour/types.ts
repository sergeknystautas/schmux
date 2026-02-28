export interface TourStep {
  /** CSS selector for the target element (prefer data-tour attributes) */
  target: string;
  /** Step title shown in tooltip */
  title: string;
  /** Step description shown in tooltip */
  body: string;
  /** Tooltip placement relative to target */
  placement: 'top' | 'bottom' | 'left' | 'right';
  /** How to advance: 'click' = user clicks target, 'next' = user clicks Next button */
  advanceOn: 'click' | 'next';
  /** Inject state changes before this step renders */
  beforeStep?: () => void;
  /** Update state after this step completes */
  afterStep?: () => void;
  /** Route to navigate to before showing this step */
  route?: string;
}

export interface TourScenario {
  id: string;
  title: string;
  description: string;
  /** Initial route for the dashboard */
  initialRoute: string;
  /** Tour steps in order */
  steps: TourStep[];
}

export interface TourContextValue {
  /** Current scenario being played, or null */
  scenario: TourScenario | null;
  /** Current step index (0-based) */
  currentStep: number;
  /** Total steps */
  totalSteps: number;
  /** Whether the tour is active */
  active: boolean;
  /** Advance to the next step */
  next: () => void;
  /** Go back to the previous step */
  prev: () => void;
  /** End the tour */
  end: () => void;
}
