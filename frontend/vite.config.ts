import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// https://vite.dev/config/
// Локально: outDir ../ui/frontend/dist. В CI: VITE_OUT_DIR=dist → frontend/dist, потом копируем в ui/frontend/dist.
export default defineConfig({
  plugins: [react()],
  build: {
    outDir: process.env.VITE_OUT_DIR || '../ui/frontend/dist',
    emptyOutDir: true,
  },
})
