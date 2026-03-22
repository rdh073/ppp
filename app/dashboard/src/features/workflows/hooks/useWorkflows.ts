import { useCallback, useMemo } from 'react';
import { listWorkflows, getWorkflow } from '../api/workflows';
import { useWorkflowStore } from '../store/workflows';
import { WORKFLOW_POLL_MS } from '../../../config';
import { usePolling } from '../../../shared/react/usePolling';

export function useWorkflows(intervalMs: number = WORKFLOW_POLL_MS) {
  const workflows = useWorkflowStore((state) => state.workflows);
  const selected = useWorkflowStore((state) => state.selected);
  const loading = useWorkflowStore((state) => state.loading);
  const error = useWorkflowStore((state) => state.error);
  const setWorkflows = useWorkflowStore((state) => state.setWorkflows);
  const setSelectedWorkflow = useWorkflowStore((state) => state.setSelectedWorkflow);
  const setLoading = useWorkflowStore((state) => state.setLoading);
  const setError = useWorkflowStore((state) => state.setError);

  const loadWorkflows = useCallback(async (silent = false) => {
    if (!silent || workflows.length === 0) {
      setLoading(true);
    }
    try {
      const payload = await listWorkflows();
      const sorted = [...payload].sort((a, b) => a.name.localeCompare(b.name));
      setWorkflows(sorted);
    } catch (raw) {
      const message = raw instanceof Error ? raw.message : 'Failed to load workflows';
      setError(message);
    }
  }, [setError, setLoading, setWorkflows, workflows.length]);

  const refresh = useCallback(() => {
    void loadWorkflows(false);
  }, [loadWorkflows]);

  const refreshSilent = useCallback(async () => {
    await loadWorkflows(true);
  }, [loadWorkflows]);

  usePolling(refreshSilent, intervalMs, {
    enabled: true,
    immediate: true,
    pauseWhenHidden: true,
  });

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
