import { Issue, Phase } from '@/lib/playerState.ts';
import { has, pick, type Bits } from '@/lib/bitflags.ts';

const status: Record<Phase, string> = {
  [Phase.Idle]: '',
  [Phase.Connecting]: 'Connecting…',
  [Phase.Playing]: 'Playing',
  [Phase.Lost]: 'Connection lost, trying to get it back…',
  [Phase.Reconnecting]: 'Reconnecting…',
  [Phase.Error]: '',
};

const button: Record<Phase, string> = {
  [Phase.Idle]: 'Listen',
  [Phase.Connecting]: 'Connecting…',
  [Phase.Playing]: 'Stop',
  [Phase.Lost]: 'Stop',
  [Phase.Reconnecting]: 'Stop',
  [Phase.Error]: 'Listen',
};

/** The line under the button. */
export function statusText(phase: Phase, error: string, issues: Bits = 0): string {
  if (phase === Phase.Error) {
    return `Could not connect: ${error}`;
  }
  if (phase === Phase.Playing && has(issues, Issue.Blocked)) {
    return 'Connected, but the browser blocked the sound.';
  }
  return pick(status, phase);
}

/** The line that says the track name may be old. */
export const noPlaylistText = 'Track information is unavailable.';

/** The text of the button. While connecting it is still pressable, and cancels. */
export function buttonLabel(phase: Phase): string {
  return pick(button, phase);
}

const onAir: Record<Phase, string> = {
  [Phase.Idle]: 'OFF AIR',
  [Phase.Connecting]: 'TUNING',
  [Phase.Playing]: 'LIVE',
  [Phase.Lost]: 'SIGNAL LOST',
  [Phase.Reconnecting]: 'RETUNING',
  [Phase.Error]: 'OFF AIR',
};

/** The word on the station's on-air badge. */
export function onAirLabel(phase: Phase): string {
  return pick(onAir, phase);
}
