import type { Device } from '../types';

export function getAndroidIdentity(device: Device): string {
  if (device.androidIdentity && device.androidIdentity.trim() !== '') {
    return device.androidIdentity.trim();
  }

  const manufacturer = device.deviceMetadata?.manufacturer?.trim();
  const model = device.deviceMetadata?.model?.trim();
  if (manufacturer && model) {
    return `${manufacturer} ${model}`.trim();
  }
  if (model) {
    return model;
  }

  if (device.observedAndroidId && device.observedAndroidId.trim() !== '') {
    return device.observedAndroidId.trim();
  }
  if (device.adbSerial && device.adbSerial.trim() !== '') {
    return device.adbSerial.trim();
  }
  return device.deviceId;
}
