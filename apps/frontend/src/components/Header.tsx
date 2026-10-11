import { Wordmark } from '@/components/Wordmark.tsx';

/** The name of the station, centred at the top of the page. */
export function Header() {
  return (
    <header className="flex w-full max-w-md items-center justify-center border-b border-slate-900/10 pt-7 pb-5">
      <h1 className="flex items-center">
        <Wordmark className="h-10 w-auto text-slate-900" />
      </h1>
    </header>
  );
}
