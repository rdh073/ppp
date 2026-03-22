import { useCallback, useRef, useState } from 'react';
import { recordStart, recordStop, recordStatus, llmRun } from '../api/recording';
import type { RecordResult, LLMRunResult } from '../api/recording';
import { usePolling } from '../../../shared/react/usePolling';

export type RunState = 'idle' | 'recording' | 'running' | 'done' | 'error';

export interface RecordOutput {
  script: string;
  workflowName: string;
  actionCount: number;
  durationMs?: number;
  steps?: number;
  reason?: string;
  done?: boolean;
}

export interface RecordingSession {
  runState: RunState;
  seq: number;
  output: RecordOutput | null;
  error: string;
  startManual: (workflowName?: string) => void;
  stopManual: (workflowName?: string) => void;
  runAI: (opts: { goal: string; maxSteps: number; workflowName?: string; timeout: number }) => void;
  cancel: () => void;
  reset: () => void;
}

export function useRecordingSession(deviceId: string): RecordingSession {
  const [runState, setRunState] = useState<RunState>('idle');
  const [seq, setSeq] = useState(0);
  const [output, setOutput] = useState<RecordOutput | null>(null);
  const [error, setError] = useState('');
  const cancelledRef = useRef(false);

  // Poll recording status while in manual recording mode
  const pollFetcher = useCallback(async () => {
    try {
      const s = await recordStatus(deviceId);
      setSeq(s.seq);
      if (!s.active) setRunState('idle');
    } catch {
      // ignore transient poll errors
    }
  }, [deviceId]);

  usePolling(pollFetcher, 2000, { enabled: runState === 'recording', immediate: false });

  const startManual = useCallback(async (workflowName?: string) => {
    setRunState('recording');
    setError('');
    try {
      await recordStart(deviceId, workflowName);
    } catch (e) {
      setRunState('idle');
      setError(e instanceof Error ? e.message : 'Failed to start recording');
    }
  }, [deviceId]);

  const stopManual = useCallback(async (workflowName?: string) => {
    setRunState('idle');
    try {
      const res: RecordResult = await recordStop(deviceId, workflowName);
      setOutput({
        script: res.script,
        workflowName: res.workflowName,
        actionCount: res.actionCount,
      });
      setRunState('done');
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to stop recording');
    }
  }, [deviceId]);

  const runAI = useCallback(async (opts: { goal: string; maxSteps: number; workflowName?: string; timeout: number }) => {
    setRunState('running');
    setError('');
    cancelledRef.current = false;
    try {
      const res: LLMRunResult = await llmRun(deviceId, opts);
      if (cancelledRef.current) return;
      setOutput({
        script: res.script,
        workflowName: res.workflowName,
        actionCount: res.actionCount,
        durationMs: res.durationMs,
        steps: res.steps,
        reason: res.reason,
        done: res.done,
      });
      setRunState('done');
    } catch (e) {
      if (cancelledRef.current) return;
      setRunState('error');
      setError(e instanceof Error ? e.message : 'LLM run failed');
    }
  }, [deviceId]);

  const cancel = useCallback(() => {
    cancelledRef.current = true;
    setRunState('idle');
    setError('Run cancelled.');
  }, []);

  const reset = useCallback(() => {
    setOutput(null);
    setError('');
    setSeq(0);
    setRunState('idle');
  }, []);

  return { runState, seq, output, error, startManual, stopManual, runAI, cancel, reset };
}
