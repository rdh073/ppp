export type TaskStatus = 'pending' | 'running' | 'paused' | 'completed' | 'failed' | 'cancelled';

export interface Device {
  deviceId: string;
  sessionId: string;
  agentInstanceId?: string;
  capabilities: Array<{ name: string; [key: string]: unknown }>;
  connectedAt: string;
  lastHeartbeatAt: string;
}

export interface TaskCreateRequest {
  goal: string;
  deviceId?: string;
  workflowName?: string;
  inputArtifacts?: Record<string, string>;
}

export interface Task {
  id: string;
  goal: string;
  inputArtifacts: Record<string, string>;
  status: TaskStatus;
  assignedDevice: string;
  createdAt: string;
  updatedAt: string;
}

export interface TargetDef {
  kind: string;
  value: string;
}

export interface WorkflowStepDef {
  type?: string;
  action?: {
    kind?: string;
    action?: string;
    package?: string;
    value?: string;
    direction?: string;
    target?: TargetDef;
    input_text?: string;
  };
  expect?: Record<string, unknown>;
  when?: string[];
  on_success?: string[];
  on_failure?: string[];
  timeout?: number;
  max_retry?: number;
}

export interface WorkflowDefinition {
  name: string;
  version?: number;
  entry?: string;
  steps?: Record<string, WorkflowStepDef>;
}

export interface EventRecord {
  event: {
    ID: string;
    Kind: string;
    DeviceID: string;
    SeqNo: number;
    OccurredAt: string;
    Payload?: unknown;
  };
  acceptedAt: string;
  source: string;
}

export interface PagedEventsResponse {
  items: EventRecord[];
  total: number;
  offset: number;
  limit: number;
  hasMore: boolean;
  nextCursor?: string;
}

export interface DeviceQueryParams {
  deviceId?: string;
  limit?: number;
  offset?: number;
  order?: 'asc' | 'desc';
  cursor?: string;
  from?: string;
  to?: string;
  kind?: string;
  source?: string;
  includePayload?: boolean;
}
