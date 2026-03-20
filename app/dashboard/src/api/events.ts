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
      deviceId: params.deviceId,
      includePayload: params.includePayload === undefined ? undefined : String(params.includePayload),
    },
  });
}

export function getAcceptedEvent(eventId: string): Promise<EventRecord> {
  return requestJson(`/events/accepted/${encodeURIComponent(eventId)}`, {
    method: 'GET',
  });
}

export interface AcceptedPayloadPreview {
  eventId: string;
  kind: string;
  deviceId: string;
  sizeBytes: number;
  truncated: boolean;
  payloadText: string;
}

export function getAcceptedEventPayload(eventId: string, maxBytes = 16_384): Promise<AcceptedPayloadPreview> {
  return requestJson(`/events/accepted/${encodeURIComponent(eventId)}/payload`, {
    method: 'GET',
    query: {
      maxBytes,
    },
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
