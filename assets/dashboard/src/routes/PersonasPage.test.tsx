import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Routes, Route } from 'react-router-dom';
import PersonasPage from './PersonasPage';

// --- Mocks ---

const mockPersonas = [
  {
    id: 'security-auditor',
    name: 'Security Auditor',
    icon: '🔒',
    color: '#e74c3c',
    prompt: 'You are a security expert.\nCheck for vulnerabilities.',
    expectations: 'Produce a structured report.',
    built_in: true,
  },
  {
    id: 'custom-persona',
    name: 'Custom Persona',
    icon: '🎯',
    color: '#3498db',
    prompt: 'You are a custom agent.',
    expectations: '',
    built_in: false,
  },
];

vi.mock('../lib/api', () => ({
  getPersonas: vi.fn(),
  createPersona: vi.fn(),
  updatePersona: vi.fn(),
  deletePersona: vi.fn(),
  getErrorMessage: (err: unknown, fallback: string) =>
    err instanceof Error ? err.message : fallback,
}));

const mockToastSuccess = vi.fn();
const mockToastError = vi.fn();

vi.mock('../components/ToastProvider', () => ({
  useToast: () => ({
    success: mockToastSuccess,
    error: mockToastError,
  }),
}));

import { getPersonas, createPersona } from '../lib/api';

const mockGetPersonas = vi.mocked(getPersonas);
const mockCreatePersona = vi.mocked(createPersona);

function renderPage(initialRoute = '/personas') {
  return render(
    <MemoryRouter initialEntries={[initialRoute]}>
      <Routes>
        <Route path="/personas" element={<PersonasPage />} />
        <Route path="/personas/:personaId" element={<PersonasPage />} />
      </Routes>
    </MemoryRouter>
  );
}

describe('PersonasPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockGetPersonas.mockResolvedValue({ personas: mockPersonas });
  });

  it('renders a list of persona cards', async () => {
    renderPage();

    await waitFor(() => {
      expect(screen.getByTestId('persona-card-security-auditor')).toBeDefined();
      expect(screen.getByTestId('persona-card-custom-persona')).toBeDefined();
    });
  });

  it('shows icon and name on each card', async () => {
    renderPage();

    await waitFor(() => {
      expect(screen.getByText('Security Auditor')).toBeDefined();
      expect(screen.getByText('🔒')).toBeDefined();
      expect(screen.getByText('Custom Persona')).toBeDefined();
      expect(screen.getByText('🎯')).toBeDefined();
    });
  });

  it('opens create form when Create Persona is clicked', async () => {
    const user = userEvent.setup();
    renderPage();

    await waitFor(() => {
      expect(screen.getByText('Create Persona')).toBeDefined();
    });

    await user.click(screen.getByText('Create Persona'));

    await waitFor(() => {
      expect(screen.getByTestId('persona-form')).toBeDefined();
    });
    expect(screen.getByLabelText('Name')).toBeDefined();
    expect(screen.getByLabelText('Personality')).toBeDefined();
  });

  it('submits create form with correct data', async () => {
    const user = userEvent.setup();
    mockCreatePersona.mockResolvedValue({
      id: 'new-persona',
      name: 'New Persona',
      icon: '✨',
      color: '#000000',
      prompt: 'Test prompt',
      expectations: '',
      built_in: false,
    });

    renderPage();

    await waitFor(() => {
      expect(screen.getByText('Create Persona')).toBeDefined();
    });

    await user.click(screen.getByText('Create Persona'));

    await waitFor(() => {
      expect(screen.getByLabelText('Name')).toBeDefined();
    });

    await user.type(screen.getByLabelText('Name'), 'New Persona');
    await user.type(screen.getByLabelText('Icon (emoji)'), '✨');
    await user.type(screen.getByLabelText('Personality'), 'Test prompt');

    await user.click(screen.getByText('Create'));

    await waitFor(() => {
      expect(mockCreatePersona).toHaveBeenCalledWith(
        expect.objectContaining({
          id: 'new-persona',
          name: 'New Persona',
          icon: '✨',
          prompt: 'Test prompt',
        })
      );
    });
  });

  it('handles loading state', () => {
    mockGetPersonas.mockReturnValue(new Promise(() => {})); // never resolves
    renderPage();
    expect(screen.getByText('Loading...')).toBeDefined();
  });

  it('handles error state', async () => {
    mockGetPersonas.mockRejectedValue(new Error('Network error'));
    renderPage();

    await waitFor(() => {
      expect(screen.getByText('Network error')).toBeDefined();
    });
  });

  it('rejects creating a persona with reserved ID "new"', async () => {
    const user = userEvent.setup();
    renderPage('/personas/new');

    await waitFor(() => {
      expect(screen.getByTestId('persona-form')).toBeDefined();
    });

    await user.type(screen.getByLabelText('Name'), 'New');
    await user.type(screen.getByLabelText('Icon (emoji)'), '🆕');
    await user.type(screen.getByLabelText('Personality'), 'Some prompt');

    await user.click(screen.getByText('Create'));

    await waitFor(() => {
      expect(mockToastError).toHaveBeenCalledWith(
        expect.stringContaining('"new" is a reserved ID')
      );
    });
    expect(mockCreatePersona).not.toHaveBeenCalled();
  });
});
