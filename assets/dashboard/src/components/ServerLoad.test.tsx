import { render, screen, fireEvent } from '@testing-library/react';
import ServerLoad from './ServerLoad';
import { updateServerLoad } from '../lib/serverLoad';

beforeEach(() => {
  updateServerLoad(null);
  localStorage.clear();
});

test('shows waiting state when no sample has arrived', () => {
  render(<ServerLoad />);
  expect(screen.getByText('Waiting for first sample…')).toBeInTheDocument();
});

test('shows formatted load values after a store update', async () => {
  render(<ServerLoad />);
  updateServerLoad({ one: 2.22, five: 3.18, fifteen: 3.28 });
  // The component polls the store version every 500ms; findBy waits for it.
  const el = await screen.findByTestId('server-load-values');
  expect(el).toHaveTextContent('Load: 2.22 3.18 3.28');
});

test('collapse toggle persists to localStorage', () => {
  render(<ServerLoad />);
  fireEvent.click(screen.getByRole('button'));
  expect(localStorage.getItem('server-load-collapsed')).toBe('1');
  fireEvent.click(screen.getByRole('button'));
  expect(localStorage.getItem('server-load-collapsed')).toBe('0');
});
