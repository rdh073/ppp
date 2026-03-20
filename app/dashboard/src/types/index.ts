export type TaskStatus = 'pending' | 'running' | 'paused' | 'completed' | 'failed' | 'cancelled';

export interface Device {
  deviceId: string;
  androidIdentity?: string;
  sessionId: string;
  agentInstanceId?: string;
  capabilities: Array<{ name: string; [key: string]: unknown }>;
  connectedAt: string;
  lastHeartbeatAt: string;
  deviceMetadata?: {
    manufacturer?: string;
    model?: string;
    device?: string;
    brand?: string;
    product?: string;
    androidVersion?: string;
    sdkInt?: number;
  };
  adbSerial?: string;
  observedAndroidId?: string;
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
  workflowName?: string;
  currentStep?: string;
  retryCount?: number;
  lastCommandStatus?: string;
  lastCommandError?: string;
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
  tool_call?: {
    tool_name?: string;
    params?: Record<string, string>;
    outputs?: Record<string, string>;
    optional?: boolean;
  };
  expect?: Record<string, unknown>;
  when?: string[];
  on_success?: string | string[];
  on_failure?: string | string[];
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
