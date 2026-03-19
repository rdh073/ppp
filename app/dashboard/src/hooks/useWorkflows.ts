import { useCallback, useMemo } from 'react';
import { listWorkflows, getWorkflow } from '../api/workflows';
import { useWorkflowStore } from '../store/workflows';
import { POLL_MS } from '../config';
import { usePolling } from './usePolling';

export function useWorkflows(intervalMs: number = POLL_MS) {
  const workflows = useWorkflowStore((state) => state.workflows);
  const selected = useWorkflowStore((state) => state.selected);
  const loading = useWorkflowStore((state) => state.loading);
  const error = useWorkflowStore((state) => state.error);
  const setWorkflows = useWorkflowStore((state) => state.setWorkflows);
  const setSelectedWorkflow = useWorkflowStore((state) => state.setSelectedWorkflow);
  const setLoading = useWorkflowStore((state) => state.setLoading);
  const setError = useWorkflowStore((state) => state.setError);

  const loadWorkflows = useCallback(async () => {
    setLoading(true);
    try {
      const payload = await listWorkflows();
      setWorkflows(payload);
    } catch (raw) {
      const message = raw instanceof Error ? raw.message : 'Failed to load workflows';
      setError(message);
    }
  }, [setError, setLoading, setWorkflows]);

  const refresh = usePolling(loadWorkflows, intervalMs, true);

  const loadWorkflowByName = useCallback(
    async (name: string) => {
      setLoading(true);
      try {
        const workflow = await getWorkflow(name);
        setSelectedWorkflow(workflow);
      } catch (raw) {
        const message = raw instanceof Error ? raw.message : 'Failed to load workflow';
        setError(message);
      }
    },
    [setError, setLoading, setSelectedWorkflow],
  );

  const workflowByName = useMemo(() => {
    return (name: string) => workflows.find((workflow) => workflow.name === name);
  }, [workflows]);

  return {
    workflows,
    selected,
    loading,
    error,
    workflowByName,
    refresh,
    loadWorkflowByName,
  };
}
