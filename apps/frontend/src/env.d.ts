/// <reference types="vite/client" />

interface ImportMetaEnv {
  /** Prefix of the server's routes. The default is /api, which the development server and the nginx image both handle. */
  readonly VITE_API_BASE?: string;
  /** The ICE servers of the browser, as JSON, for example `[{"urls":"stun:stun.l.google.com:19302"}]`. The default is none. */
  readonly VITE_ICE_SERVERS?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
