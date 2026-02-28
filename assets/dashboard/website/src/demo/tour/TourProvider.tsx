import { createContext, useContext, useState, useCallback, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import Spotlight from './Spotlight';
import Tooltip from './Tooltip';
import type { TourScenario, TourContextValue } from './types';
import './tour.css';

const TourContext = createContext<TourContextValue | null>(null);

export function useTour(): TourContextValue {
  const ctx = useContext(TourContext);
  if (!ctx) throw new Error('useTour must be used within TourProvider');
  return ctx;
}

interface TourProviderProps {
  scenario: TourScenario;
  children: React.ReactNode;
}

export default function TourProvider({ scenario, children }: TourProviderProps) {
  const [currentStep, setCurrentStep] = useState(0);
  const [active, setActive] = useState(true);
  const navigate = useNavigate();

  const step = scenario.steps[currentStep];

  // Run beforeStep hook when step changes
  useEffect(() => {
    if (!active || !step) return;
    step.beforeStep?.();
    if (step.route) navigate(step.route);
  }, [currentStep, active]); // eslint-disable-line react-hooks/exhaustive-deps

  // For 'click' advance mode, listen for clicks on the target.
  // Uses MutationObserver to handle elements that appear asynchronously.
  useEffect(() => {
    if (!active || !step || step.advanceOn !== 'click') return;
    let boundEl: Element | null = null;
    const handler = () => {
      step.afterStep?.();
      if (currentStep < scenario.steps.length - 1) {
        setCurrentStep((s) => s + 1);
      } else {
        setActive(false);
      }
    };
    const tryBind = () => {
      const el = document.querySelector(step.target);
      if (el && el !== boundEl) {
        boundEl?.removeEventListener('click', handler);
        boundEl = el;
        el.addEventListener('click', handler, { once: true });
      }
    };
    tryBind();
    const observer = new MutationObserver(tryBind);
    observer.observe(document.body, { childList: true, subtree: true, attributes: true });
    return () => {
      observer.disconnect();
      boundEl?.removeEventListener('click', handler);
    };
  }, [active, step, currentStep, scenario.steps.length]);

  const next = useCallback(() => {
    step?.afterStep?.();
    if (currentStep < scenario.steps.length - 1) {
      setCurrentStep((s) => s + 1);
    } else {
      setActive(false);
    }
  }, [currentStep, scenario.steps.length, step]);

  const prev = useCallback(() => {
    if (currentStep > 0) setCurrentStep((s) => s - 1);
  }, [currentStep]);

  const end = useCallback(() => setActive(false), []);

  const value: TourContextValue = {
    scenario,
    currentStep,
    totalSteps: scenario.steps.length,
    active,
    next,
    prev,
    end,
  };

  return (
    <TourContext.Provider value={value}>
      {children}
      {active && step && (
        <>
          <Spotlight target={step.target} active={active} />
          <Tooltip
            step={step}
            currentStep={currentStep}
            totalSteps={scenario.steps.length}
            onNext={next}
            onPrev={prev}
            onEnd={end}
          />
        </>
      )}
    </TourContext.Provider>
  );
}
