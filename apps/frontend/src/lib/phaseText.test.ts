import { describe, expect, it } from 'vitest';

import { buttonLabel, onAirLabel, statusText } from '@/lib/phaseText.ts';
import {
  Connection,
  Issue,
  Listening,
  Phase,
  Stage,
  Stopped,
  Trouble,
  connectionOf,
  stageOf,
} from '@/lib/playerState.ts';
import { has } from '@/lib/bitflags.ts';

describe('statusText and buttonLabel', () => {
  it('say what the screens of the user path say', () => {
    expect(statusText(Phase.Idle, '')).toBe('');
    expect(statusText(Phase.Connecting, '')).toBe('Connecting…');
    expect(statusText(Phase.Playing, '')).toBe('Playing');
    expect(statusText(Phase.Lost, '')).toBe('Connection lost, trying to get it back…');
    expect(statusText(Phase.Reconnecting, '')).toBe('Reconnecting…');
    expect(statusText(Phase.Error, 'engine: closed')).toBe('Could not connect: engine: closed');

    expect([Phase.Idle, Phase.Error].map(buttonLabel)).toEqual(['Listen', 'Listen']);
    expect(buttonLabel(Phase.Connecting)).toBe('Connecting…');
    expect([Phase.Playing, Phase.Lost, Phase.Reconnecting].map(buttonLabel)).toEqual(['Stop', 'Stop', 'Stop']);
  });
});

describe('statusText with issues', () => {
  it('says that the sound is blocked, only while playing', () => {
    expect(statusText(Phase.Playing, '', Issue.Blocked)).toBe('Connected, but the browser blocked the sound.');
    expect(statusText(Phase.Playing, '', Issue.NoStats)).toBe('Playing');
    expect(statusText(Phase.Connecting, '', Issue.Blocked)).toBe('Connecting…');
  });
});

describe('onAirLabel', () => {
  it('says what the station is doing in the words of a radio', () => {
    expect(onAirLabel(Phase.Idle)).toBe('OFF AIR');
    expect(onAirLabel(Phase.Error)).toBe('OFF AIR');
    expect(onAirLabel(Phase.Connecting)).toBe('TUNING');
    expect(onAirLabel(Phase.Playing)).toBe('LIVE');
    expect(onAirLabel(Phase.Lost)).toBe('SIGNAL LOST');
    expect(onAirLabel(Phase.Reconnecting)).toBe('RETUNING');
  });
});

describe('the bits', () => {
  it('give every phase and every connection state a bit of its own', () => {
    for (const set of [Object.values(Phase), Object.values(Connection)]) {
      const all = set.reduce((sum, bit) => sum | bit, 0);
      expect(all).toBe(2 ** set.length - 1);
    }
  });

  it('split the phases into the ones that listen and the ones that do not', () => {
    expect(has(Listening, Phase.Playing) && has(Listening, Phase.Lost) && has(Listening, Phase.Reconnecting)).toBe(
      true,
    );
    expect(has(Listening, Phase.Connecting) || has(Listening, Stopped)).toBe(false);
    expect(has(Stopped, Phase.Idle) && has(Stopped, Phase.Error)).toBe(true);
    expect(has(Trouble, Phase.Lost) && has(Trouble, Phase.Reconnecting)).toBe(true);
  });

  it('group the phases into stages', () => {
    expect([Phase.Idle, Phase.Error].map(stageOf)).toEqual([Stage.Stopped, Stage.Stopped]);
    expect([Phase.Lost, Phase.Reconnecting].map(stageOf)).toEqual([Stage.Trouble, Stage.Trouble]);
    expect(stageOf(Phase.Connecting)).toBe(Stage.Connecting);
    expect(stageOf(Phase.Playing)).toBe(Stage.Playing);
  });

  it('turn the state names of the browser into bits', () => {
    expect(connectionOf('connected')).toBe(Connection.Connected);
    expect(connectionOf('failed')).toBe(Connection.Failed);
    expect(connectionOf('disconnected')).toBe(Connection.Disconnected);
  });
});
