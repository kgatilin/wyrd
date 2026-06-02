import { describe, it, expect } from 'vitest';
import { render, screen, act } from '@testing-library/react';
import { createStore } from './store';
import { initialState, type AppState } from '../domain/state';
import type { Event } from '../domain/events';
import { update } from '../domain/update';
import { StoreProvider, useStore, useDispatch } from './react';

function Probe() {
  const focusId = useStore((s: AppState) => s.ui.focusId);
  const dispatch = useDispatch();
  return (
    <button onClick={() => dispatch({ type: 'ComponentSelected', id: 'a' })}>
      {focusId ?? 'none'}
    </button>
  );
}

describe('react binding', () => {
  it('renders selected state and re-renders on dispatch', () => {
    const store = createStore<AppState, Event>(initialState, update, []);
    render(
      <StoreProvider store={store}>
        <Probe />
      </StoreProvider>
    );
    expect(screen.getByRole('button').textContent).toBe('none');
    act(() => {
      store.dispatch({ type: 'ComponentSelected', id: 'a' });
    });
    expect(screen.getByRole('button').textContent).toBe('a');
  });
});
