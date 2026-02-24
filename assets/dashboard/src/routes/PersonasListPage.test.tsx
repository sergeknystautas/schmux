import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Routes, Route } from 'react-router-dom';
import PersonasListPage from './PersonasListPage';
import PersonaCreatePage from './PersonaCreatePage';

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

function renderListPage(initialRoute = '/personas') {
  return render(
    <MemoryRouter initialEntries={[initialRoute]}>
      <Routes>
        <Route path="/personas" element={<PersonasListPage />} />
        <Route path="/personas/create" element={<PersonaCreatePage />} />
      </Routes>
    </MemoryRouter>
  );
}

function renderCreatePage() {
  return render(
    <MemoryRouter initialEntries={['/personas/create']}>
      <Routes>
        <Route path="/personas" element={<PersonasListPage />} />
        <Route path="/personas/create" element={<PersonaCreatePage />} />
      </Routes>
    </MemoryRouter>
  );
}

describe('PersonasListPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockGetPersonas.mockResolvedValue({ personas: mockPersonas });
  });

  it('renders a list of persona cards', async () => {
    renderListPage();

    await waitFor(() => {
      expect(screen.getByTestId('persona-card-security-auditor')).toBeDefined();
      expect(screen.getByTestId('persona-card-custom-persona')).toBeDefined();
    });
  });

  it('shows icon and name on each card', async () => {
    renderListPage();

    await waitFor(() => {
      expect(screen.getByText('Security Auditor')).toBeDefined();
      expect(screen.getByText('🔒')).toBeDefined();
      expect(screen.getByText('Custom Persona')).toBeDefined();
      expect(screen.getByText('🎯')).toBeDefined();
    });
  });

  it('navigates to create form when Create Persona is clicked', async () => {
    const user = userEvent.setup();
    renderListPage();

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

  it('handles loading state', () => {
    mockGetPersonas.mockReturnValue(new Promise(() => {})); // never resolves
    renderListPage();
    expect(screen.getByText('Loading...')).toBeDefined();
  });

  it('handles error state', async () => {
    mockGetPersonas.mockRejectedValue(new Error('Network error'));
    renderListPage();

    await waitFor(() => {
      expect(screen.getByText('Network error')).toBeDefined();
    });
  });
});

describe('PersonaCreatePage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockGetPersonas.mockResolvedValue({ personas: mockPersonas });
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

    renderCreatePage();

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

  it('rejects creating a persona with reserved ID "create"', async () => {
    const user = userEvent.setup();
    renderCreatePage();

    await waitFor(() => {
      expect(screen.getByTestId('persona-form')).toBeDefined();
    });

    await user.type(screen.getByLabelText('Name'), 'Create');
    await user.type(screen.getByLabelText('Icon (emoji)'), '🆕');
    await user.type(screen.getByLabelText('Personality'), 'Some prompt');

    await user.click(screen.getByText('Create'));

    await waitFor(() => {
      expect(mockToastError).toHaveBeenCalledWith(
        expect.stringContaining('"create" is a reserved ID')
      );
    });
    expect(mockCreatePersona).not.toHaveBeenCalled();
  });
});
