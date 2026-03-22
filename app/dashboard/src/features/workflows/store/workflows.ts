import { create } from 'zustand';
import type { WorkflowDefinition } from '../../../types';
import { createLoadableActions } from '../../../shared/state/helpers';

interface WorkflowState {
  workflows: WorkflowDefinition[];
  selected?: WorkflowDefinition | null;
  loading: boolean;
  error: string | null;
  setWorkflows: (workflows: WorkflowDefinition[]) => void;
  setSelectedWorkflow: (workflow: WorkflowDefinition | null) => void;
  setLoading: (loading: boolean) => void;
  setError: (error: string | null) => void;
}

export const useWorkflowStore = create<WorkflowState>((set) => ({
  ...createLoadableActions<WorkflowState>(set),
  workflows: [],
  selected: null,
  loading: false,
  error: null,
  setWorkflows: (workflows) =>
    set({
      workflows,
      loading: false,
      error: null,
    }),
  setSelectedWorkflow: (selected) =>
    set({
      selected,
      loading: false,
      error: null,
    }),
}));
