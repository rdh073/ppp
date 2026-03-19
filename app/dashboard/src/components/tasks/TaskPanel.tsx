import { FormEvent, useMemo, useState } from 'react';
import { useTasks } from '../../hooks/useTasks';
import { useDevices } from '../../hooks/useDevices';
import { useWorkflows } from '../../hooks/useWorkflows';
import { POLL_MS } from '../../config';

export function TaskPanel() {
  const { tasks, loading, loadingById, error, create, refreshById, cancel } = useTasks();
  const { devices } = useDevices(POLL_MS);
  const { workflows } = useWorkflows(Math.max(10_000, POLL_MS));

  const [goal, setGoal] = useState('');
  const [deviceId, setDeviceId] = useState('');
  const [workflowName, setWorkflowName] = useState('');
  const [inputArtifacts, setInputArtifacts] = useState('{}');
  const [taskLookupId, setTaskLookupId] = useState('');

  const parsedArtifacts = useMemo(() => {
    try {
      const parsed = JSON.parse(inputArtifacts);
      if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
        return parsed as Record<string, string>;
      }
      return {};
    } catch {
      return null;
    }
  }, [inputArtifacts]);

  const onSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();

    if (!goal.trim()) {
      return;
    }

    try {
      await create({
        goal: goal.trim(),
        deviceId: deviceId.trim() || undefined,
        workflowName: workflowName.trim() || undefined,
        inputArtifacts: parsedArtifacts || undefined,
      });
      setGoal('');
      setWorkflowName('');
      setDeviceId('');
      setInputArtifacts('{}');
    } catch {
      // error is handled in store
    }
  };

  return (
    <section className="panel">
      <div className="panel-header">
        <h2>Tasks</h2>
      </div>

      <form className="task-form" onSubmit={onSubmit}>
        <label>
          Goal
          <input value={goal} onChange={(e) => setGoal(e.target.value)} placeholder="automation goal" required />
        </label>
        <label>
          Device ID (optional)
          <select value={deviceId} onChange={(e) => setDeviceId(e.target.value)}>
            <option value="">Auto-assign device</option>
            {devices.map((device) => (
              <option key={device.deviceId} value={device.deviceId}>
                {device.deviceId}
              </option>
            ))}
          </select>
        </label>
        <label>
          Workflow (optional)
          <select value={workflowName} onChange={(e) => setWorkflowName(e.target.value)}>
            <option value="">Auto-select workflow</option>
            {workflows.map((workflow) => (
              <option key={workflow.name} value={workflow.name}>
                {workflow.name}
              </option>
            ))}
          </select>
        </label>
        <label>
          inputArtifacts (JSON)
          <textarea
            value={inputArtifacts}
            onChange={(e) => setInputArtifacts(e.target.value)}
            rows={4}
          />
        </label>
        <div className="actions">
          <button type="submit" disabled={loading || parsedArtifacts === null}>Create task</button>
        </div>
      </form>

      <div className="panel-subhead">Task lookup</div>
      <div className="row">
        <input
          value={taskLookupId}
          onChange={(event) => setTaskLookupId(event.target.value)}
          placeholder="task-id"
        />
        <button
          type="button"
          onClick={() => {
            if (taskLookupId.trim()) {
              refreshById(taskLookupId.trim());
            }
          }}
          disabled={loadingById[taskLookupId]}
        >
          {loadingById[taskLookupId] ? 'Loading...' : 'Load by ID'}
        </button>
      </div>

      {error && <p className="error">{error}</p>}

      <div className="table-wrap">
        <table>
          <thead>
            <tr>
              <th>ID</th>
              <th>Status</th>
              <th>Goal</th>
              <th>Device</th>
              <th>Created</th>
              <th>Updated</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            {tasks.length === 0 && !loading ? (
              <tr>
                <td colSpan={7}>No task loaded yet.</td>
              </tr>
            ) : (
              tasks.map((task) => (
                <tr key={task.id}>
                  <td>{task.id}</td>
                  <td>{task.status}</td>
                  <td>{task.goal}</td>
                  <td>{task.assignedDevice || '-'}</td>
                  <td>{new Date(task.createdAt).toLocaleString()}</td>
                  <td>{new Date(task.updatedAt).toLocaleString()}</td>
                  <td>
                    <div className="actions">
                      <button
                        type="button"
                        disabled={loadingById[task.id]}
                        onClick={() => refreshById(task.id)}
                      >
                        {loadingById[task.id] ? 'Refreshing...' : 'Refresh'}
                      </button>
                      <button
                        type="button"
                        disabled={loadingById[task.id] || task.status === 'cancelled' || task.status === 'completed' || task.status === 'failed'}
                        onClick={() => {
                          void cancel(task.id);
                        }}
                      >
                        Cancel
                      </button>
                    </div>
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>
    </section>
  );
}
