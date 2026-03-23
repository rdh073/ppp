import { requestJson } from '../../../shared/http/client';

export type AccountCreationStatus = 'running' | 'done' | 'failed';
export type AccountCreationPhase = 'google' | 'instagram';

export interface AccountCreation {
  id: string;
  kind: string;
  deviceId: string;
  personaId?: string;
  status: AccountCreationStatus;
  phase?: AccountCreationPhase;
  googleTaskId?: string;
  instagramTaskId?: string;
  googleAccountId?: string;
  error?: string;
  createdAt: string;
  updatedAt: string;
}

export interface StartAccountCreationRequest {
  kind?: 'google' | 'instagram' | 'google+instagram';
  deviceId: string;
  personaId?: string;
  phoneNumber?: string;
  captchaEndpoint?: string;
}

export function listAccountCreations(): Promise<AccountCreation[]> {
  return requestJson('/account-manager/account-creations');
}

export function startAccountCreation(req: StartAccountCreationRequest): Promise<{ accountCreationId: string }> {
  return requestJson('/account-manager/account-creations', { method: 'POST', body: req });
}

export function getAccountCreation(id: string): Promise<AccountCreation> {
  return requestJson(`/account-manager/account-creations/${id}`);
}
