import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  server: { port: 5173 },
  // El panel compilado se incrusta en el binario de Go (server/web/dist).
  build: { outDir: 'server/web/dist', emptyOutDir: true, sourcemap: false },
});
