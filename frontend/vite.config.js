import { defineConfig } from 'vite';

// The backend only accepts a player login or remembered-session request whose
// Origin matches the Host it receives, and the remembered-session cookie is
// host-only. So the dev proxy must keep the browser's Host header. Vite's string
// shorthand ('/api': 'http://...') silently sets changeOrigin: true, which
// rewrites Host to the target and makes every browser login fail with 403, so
// the object form with changeOrigin: false is required here.
export default defineConfig({
  server: {
    proxy: {
      '/api/mqtt': { target: 'ws://127.0.0.1:8080', ws: true, changeOrigin: false },
      '/api': { target: 'http://127.0.0.1:8080', changeOrigin: false },
    },
  },
});
