import { Backdrop } from '@/components/Backdrop.tsx';
import { Card } from '@/components/Card.tsx';
import { Header } from '@/components/Header.tsx';
import { ListenButton } from '@/components/ListenButton.tsx';
import { PlayingBar } from '@/components/PlayingBar.tsx';
import { Presenter } from '@/components/Presenter.tsx';
import { Stats } from '@/components/Stats.tsx';
import { Station } from '@/components/Station.tsx';
import { Status } from '@/components/Status.tsx';
import { TrackList } from '@/components/TrackList.tsx';
import { usePlayer } from '@/hooks/usePlayer.ts';

export function App() {
  const { player, audio } = usePlayer();
  return (
    <div className="flex min-h-screen flex-col items-center justify-between p-4 font-sans">
      <Backdrop />
      <Header />
      <main className="my-auto flex w-full max-w-md flex-col items-center py-8">
        <Card>
          <PlayingBar />
          <Station />
          <div className="mt-6 w-full">
            <ListenButton onListen={() => player.start()} onStop={() => player.stop()} />
          </div>
          <Status onResume={() => player.resume()} />
          <Presenter />
          <Stats />
        </Card>
        <TrackList />
      </main>
      <audio ref={audio} autoPlay playsInline />
    </div>
  );
}
