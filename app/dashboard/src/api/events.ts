import { requestJson } from './client';
import type { DeviceQueryParams, EventRecord, PagedEventsResponse } from '../types';

export type EventListType = 'accepted' | 'deadletters';

export function listAcceptedEvents(params: DeviceQueryParams = {}): Promise<PagedEventsResponse> {
  return requestJson('/events/accepted', {
    method: 'GET',
    query: {
      limit: params.limit,
      offset: params.offset,
      order: params.order,
      cursor: params.cursor,
      from: params.from,
      to: params.to,
      kind: params.kind,
      source: params.source,
    },
  });
}

export function getAcceptedEvent(eventId: string): Promise<{ event: EventRecord; acceptedAt: string; source: string }> {
  return requestJson(`/events/accepted/${encodeURIComponent(eventId)}`, {
    method: 'GET',
  });
}

export function replayAcceptedEvent(eventId: string): Promise<void> {
  return requestJson(`/events/accepted/${encodeURIComponent(eventId)}/replay`, {
    method: 'POST',
  });
}

export function listDeadLetters(params: DeviceQueryParams = {}): Promise<PagedEventsResponse> {
  return requestJson('/events/deadletters', {
    method: 'GET',
    query: {
      limit: params.limit,
      offset: params.offset,
      order: params.order,
      cursor: params.cursor,
      from: params.from,
      to: params.to,
      kind: params.kind,
      source: params.source,
    },
  });
}

export function getDeadLetter(eventId: string): Promise<unknown> {
  return requestJson(`/events/deadletters/${encodeURIComponent(eventId)}`, {
    method: 'GET',
  });
}

export function replayDeadLetter(eventId: string): Promise<void> {
  return requestJson(`/events/deadletters/${encodeURIComponent(eventId)}/replay`, {
    method: 'POST',
  });
}
