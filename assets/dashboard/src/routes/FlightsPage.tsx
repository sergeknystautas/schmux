import { useState } from 'react';
import { createFlight, getErrorMessage, getFlight, reorderFlights, updateFlight } from '../lib/api';
import { useModal } from '../components/ModalProvider';
import { useToast } from '../components/ToastProvider';
import { useSessions } from '../contexts/SessionsContext';
import { flightLabel } from '../lib/flights';
import useLocalStorage from '../hooks/useLocalStorage';
import type {
  CreateFlightRequest,
  FlightDetailResponse,
  FlightSummaryResponse,
} from '../lib/types.generated';
import FlightEditForm from './flights/FlightEditForm';
import FlightForm, { type FlightDraft, type FlightFormValues } from './flights/FlightForm';
import FlightList from './flights/FlightList';
import useFlightFormOptions from './flights/useFlightFormOptions';

export default function FlightsPage() {
  const { flights, loading } = useSessions();
  const { repos, repoNames, targets, loading: configLoading } = useFlightFormOptions();
  const { alert } = useModal();
  const { success: toastSuccess } = useToast();
  const [creating, setCreating] = useState(false);
  const [editing, setEditing] = useState<FlightDetailResponse | null>(null);

  const [lastSpawnTargets] = useLocalStorage<Record<string, number>>(
    'spawn-last-target-counts',
    {}
  );
  const defaultModel = targets.find((t) => (lastSpawnTargets[t.id] ?? 0) > 0)?.id ?? '';
  const [draft, setDraft] = useLocalStorage<FlightDraft | null>('flight-new-draft', null);

  const handleCreate = async (req: CreateFlightRequest) => {
    const flight = await createFlight(req);
    setDraft({
      ...req,
      prompt: '',
      short_name: '',
      executorSelected: draft?.executorSelected ?? false,
    });
    toastSuccess(`Created ${flightLabel(flight)}`);
    setCreating(false);
  };

  const handleEdit = async (flight: FlightSummaryResponse) => {
    // The summary carries no prompt; the edit form needs the full record.
    try {
      const detail = await getFlight(flight.id);
      setEditing(detail);
    } catch (err) {
      alert('Edit failed', getErrorMessage(err, 'Failed to load flight'));
    }
  };

  const handleUpdate = async (req: FlightFormValues) => {
    if (!editing) return;
    await updateFlight(editing.id, req);
    toastSuccess(`Updated ${req.short_name || editing.id}`);
    setEditing(null);
  };

  const handleMove = async (id: string, delta: -1 | 1) => {
    const order = flights
      .filter((f) => f.workflow_state === 'backlog')
      .sort((a, b) => a.order - b.order)
      .map((f) => f.id);
    const from = order.indexOf(id);
    const to = from + delta;
    if (from < 0 || to < 0 || to >= order.length) return;
    [order[from], order[to]] = [order[to], order[from]];
    try {
      await reorderFlights(order);
    } catch (err) {
      alert('Reorder failed', getErrorMessage(err, 'Failed to reorder the backlog'));
    }
  };

  return (
    <>
      <div className="app-header">
        <div className="app-header__info">
          <h1 className="app-header__meta">
            {creating
              ? 'What do you want me to do?'
              : editing
                ? `Edit ${flightLabel(editing)}`
                : 'Flights'}
          </h1>
        </div>
        {!creating && !editing && (
          <div className="app-header__actions">
            <button
              type="button"
              className="btn btn--primary"
              onClick={() => setCreating(true)}
              disabled={configLoading}
              data-testid="flight-new"
            >
              New flight
            </button>
          </div>
        )}
      </div>

      {editing ? (
        <div data-testid="flight-edit">
          <FlightEditForm
            key={editing.id}
            repos={repos}
            targets={targets}
            flight={editing}
            onSubmit={handleUpdate}
            onCancel={() => setEditing(null)}
          />
        </div>
      ) : creating ? (
        <div className="spawn-content" data-testid="flight-create">
          <FlightForm
            repos={repos}
            targets={targets}
            draft={
              draft ?? {
                repo: repos[0]?.url ?? '',
                prompt: '',
                short_name: '',
                planner_model: defaultModel,
                executor_model: defaultModel,
                executorSelected: false,
              }
            }
            onDraftChange={setDraft}
            submitLabel="Create flight"
            onSubmit={handleCreate}
            onCancel={() => setCreating(false)}
          />
        </div>
      ) : (
        <div className="flex-col gap-lg">
          {loading ? (
            <div className="loading-state">
              <div className="spinner"></div>
            </div>
          ) : (
            <FlightList
              flights={flights}
              repoNames={repoNames}
              onMove={handleMove}
              onEdit={handleEdit}
            />
          )}
        </div>
      )}
    </>
  );
}
