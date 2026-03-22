import { describe, expect, it } from 'vitest';
import { createLoadableActions, createPersistOptions } from './helpers';

describe('store helpers', () => {
  it('createLoadableActions updates loading and error state consistently', () => {
    type State = {
      loading: boolean;
      error: string | null;
      count: number;
    };

    let state: State = {
      loading: false,
      error: null,
      count: 1,
    };

    const set = (partial: Partial<State>) => {
      state = { ...state, ...partial };
    };

    const loadable = createLoadableActions<State>(set);

    loadable.setLoading(true);
    expect(state.loading).toBe(true);
    expect(state.error).toBeNull();

    loadable.setError('boom');
    expect(state.loading).toBe(false);
    expect(state.error).toBe('boom');
  });

  it('createPersistOptions returns persist config with name and partialize', () => {
    type State = { items: string[]; loading: boolean };
    const options = createPersistOptions<State>('ppp.test.store', (state) => ({ items: state.items }));

    expect(options.name).toBe('ppp.test.store');
    expect('storage' in options).toBe(true);
    expect(options.partialize?.({ items: ['a'], loading: true } as State)).toEqual({ items: ['a'] });
  });
});
