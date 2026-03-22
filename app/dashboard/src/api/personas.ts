import { requestJson } from './client';

export interface Persona {
  id: string;
  kind: 'google' | 'instagram';
  firstName: string;
  lastName: string;
  gender?: string;
  birthDate?: string;
  email?: string;
  username?: string;
  password?: string;
  status: 'available' | 'in_use' | 'used' | 'failed';
  createdAt: string;
  updatedAt: string;
}

export interface CreatePersonaRequest {
  kind?: 'google' | 'instagram';
  firstName: string;
  lastName: string;
  gender?: string;
  birthDate?: string;
  email?: string;
  username?: string;
  password?: string;
}

export function listPersonas(params?: { kind?: string; status?: string }): Promise<Persona[]> {
  return requestJson('/personas', { query: params });
}

export function createPersona(req: CreatePersonaRequest): Promise<Persona> {
  return requestJson('/personas', { method: 'POST', body: req });
}

export function deletePersona(id: string): Promise<void> {
  return requestJson(`/personas/${id}`, { method: 'DELETE' });
}
