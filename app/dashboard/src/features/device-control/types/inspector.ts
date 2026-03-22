/** Wire-format types for the UiSnapshot returned by POST /devices/{id}/observe. */

export interface UiTarget {
  targetId: string;
  role: string | null;
  uiRole: string;
  label: string | null;
  semanticKey: string | null;
  formKey: string | null;
  text: string | null;
  resourceId: string | null;
  packageName: string | null;
  bounds: [number, number, number, number]; // [left, top, right, bottom] screen px
  actionable: boolean;
  enabled: boolean;
  checked: boolean | null;
  selected: boolean;
  scrollable: boolean;
  focused: boolean;
  password: boolean;
}

export interface UiSemanticState {
  activeUiKey: string;
  baseScreenKey: string;
  overlayKey: string | null;
  uiReady: boolean;
  semanticDigest: string;
  focusedTargetKey: string | null;
  forms: Array<{
    formKey: string;
    fieldKeys: string[];
    focusedFieldKey: string | null;
    ready: boolean;
  }>;
  buttons: Array<{
    buttonKey: string;
    enabled: boolean;
    visible: boolean;
    primary: boolean;
  }>;
}

export interface UiSnapshot {
  snapshotId: string;
  deviceId: string;
  packageName: string | null;
  activityName: string | null;
  screenState: string | null;
  focusedTargetId: string | null;
  semantic: UiSemanticState;
  capturedAt: string;
  targets: UiTarget[];
}
