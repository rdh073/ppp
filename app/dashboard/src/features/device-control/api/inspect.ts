import { requestJson } from '../../../shared/http/client';
import type { UiSnapshot } from '../types/inspector';

export function observeDevice(deviceId: string): Promise<UiSnapshot> {
  return requestJson(`/devices/${encodeURIComponent(deviceId)}/observe`, {
    method: 'POST',
    timeoutMs: 15_000,
  });
}
