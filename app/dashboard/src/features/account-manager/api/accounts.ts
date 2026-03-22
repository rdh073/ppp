import { requestJson } from '../../../shared/http/client';

export interface Account {
  id: string;
  kind: 'google' | 'instagram';
  deviceId: string;
  personaId?: string;
  email?: string;
  username?: string;
  password?: string;
  linkedAccountId?: string;
  status: 'active' | 'failed' | 'banned';
  createdAt: string;
}

export function listAccounts(params?: { kind?: string; deviceId?: string }): Promise<Account[]> {
  return requestJson('/account-manager/accounts', { query: params });
}
