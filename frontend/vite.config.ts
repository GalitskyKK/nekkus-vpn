import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// https://vite.dev/config/
// outDir: сборка сразу в ui/frontend/dist — не нужно копировать вручную
export default defineConfig({
  plugins: [react()],
  build: {
    outDir: '../ui/frontend/dist',
    emptyOutDir: true,
  },
})
