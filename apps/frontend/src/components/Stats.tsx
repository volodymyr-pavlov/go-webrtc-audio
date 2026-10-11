import { Connection, Issue } from '@/lib/playerState.ts';
import { has, pick } from '@/lib/bitflags.ts';
import { usePlayerStore } from '@/store/player.ts';

/** What shows in place of a number that cannot be read now. */
const unavailable = '—';

const connectionName: Record<Connection, string> = {
  [Connection.Idle]: 'idle',
  [Connection.New]: 'new',
  [Connection.Connecting]: 'connecting',
  [Connection.Connected]: 'connected',
  [Connection.Disconnected]: 'disconnected',
  [Connection.Failed]: 'failed',
  [Connection.Closed]: 'closed',
};

function connectionTone(connection: Connection): string {
  if (connection === Connection.Connected) {
    return 'text-emerald-700';
  }
  return connection === Connection.Idle ? 'text-slate-600' : 'text-amber-700';
}

function Stat({ name, value, tone = 'text-slate-800' }: { name: string; value: string; tone?: string }) {
  return (
    <div>
      <dt className="text-[11px] font-semibold tracking-wider text-slate-600 uppercase">{name}</dt>
      <dd className={`mt-0.5 font-mono text-sm ${tone}`}>{value}</dd>
    </div>
  );
}

/** The numbers behind the stream: the state of the connection and the packets received and lost. */
export function Stats() {
  const connection = usePlayerStore((s) => s.connection);
  const packets = usePlayerStore((s) => s.packets);
  const lost = usePlayerStore((s) => s.lost);
  const stale = has(
    usePlayerStore((s) => s.issues),
    Issue.NoStats,
  );
  return (
    <dl className="mt-5 grid w-full grid-cols-3 gap-2 border-t border-slate-200 pt-4 text-center">
      <Stat
        name="Connection"
        value={stale ? unavailable : pick(connectionName, connection)}
        tone={connectionTone(connection)}
      />
      <Stat name="Packets" value={stale ? unavailable : packets.toLocaleString()} />
      <Stat name="Lost" value={stale ? unavailable : lost.toLocaleString()} />
    </dl>
  );
}
