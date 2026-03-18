import { useState } from 'react';
import { useWorkflows } from '../../hooks/useWorkflows';

export function WorkflowPanel() {
  const { workflows, selected, loading, error, loadWorkflowByName } = useWorkflows(10000);
  const [selectedName, setSelectedName] = useState('');

  return (
    <section className="panel">
      <div className="panel-header">
        <h2>Workflows</h2>
        <button
          type="button"
          onClick={() => {
            if (selectedName) {
              void loadWorkflowByName(selectedName);
            }
          }}
        >
          Load
        </button>
      </div>

      <div className="row">
        <label>
          Name
          <input
            value={selectedName}
            onChange={(event) => setSelectedName(event.target.value)}
            placeholder="workflow name"
          />
        </label>
      </div>

      {error && <p className="error">{error}</p>}
      {loading && <p>Loading workflows...</p>}

      <div className="workflow-grid">
        <div>
          <h3>Loaded workflows</h3>
          <ul>
            {workflows.length === 0 ? (
              <li>No workflow definitions available.</li>
            ) : (
              workflows.map((workflow) => (
                <li key={workflow.name}>
                  <button
                    type="button"
                    onClick={() => {
                      loadWorkflowByName(workflow.name);
                      setSelectedName(workflow.name);
                    }}
                  >
                    {workflow.name}
                  </button>
                </li>
              ))
            )}
          </ul>
        </div>

        <div>
          <h3>Definition</h3>
          <pre>{selected ? JSON.stringify(selected, null, 2) : 'Select a workflow to view JSON.'}</pre>
        </div>
      </div>
    </section>
  );
}
