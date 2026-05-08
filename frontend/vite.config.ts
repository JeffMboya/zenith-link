import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import cesium from 'vite-plugin-cesium'

export default defineConfig({
  plugins: [react(), cesium()],
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./src/test/setup.ts'],
  },
  server: {
    port: 5173,
    proxy: {
      '/ws': { target: 'ws://localhost:8081', ws: true },
      '/metrics': 'http://localhost:8081',
      '/command': 'http://localhost:8081',
      '/health': 'http://localhost:8081',
      '/windows': 'http://localhost:8080',
      '/state': 'http://localhost:8080',
      '/track': 'http://localhost:8080',
      '/constellation': 'http://localhost:8080',
      '/tle': 'http://localhost:8080',
      '/events': 'http://localhost:8080',
      '/payload': 'http://localhost:8080',
      '/inference': 'http://localhost:8080',
      '/relay/health': {
        target: 'http://localhost:8082',
        rewrite: (path: string) => path.replace(/^\/relay/, ''),
      },
      '/relay/telemetry': {
        target: 'http://localhost:8082',
        rewrite: (path: string) => path.replace(/^\/relay/, ''),
      },
      '/relay/windows': {
        target: 'http://localhost:8082',
        rewrite: (path: string) => path.replace(/^\/relay/, ''),
      },
      '/relay2/health': {
        target: 'http://localhost:8083',
        rewrite: (path: string) => path.replace(/^\/relay2/, ''),
      },
      '/relay2/telemetry': {
        target: 'http://localhost:8083',
        rewrite: (path: string) => path.replace(/^\/relay2/, ''),
      },
      '/relay2/windows': {
        target: 'http://localhost:8083',
        rewrite: (path: string) => path.replace(/^\/relay2/, ''),
      },
      '/relay3': { target: 'http://localhost:8089', rewrite: (p: string) => p.replace(/^\/relay3/, '') },
      '/relay4': { target: 'http://localhost:8090', rewrite: (p: string) => p.replace(/^\/relay4/, '') },
      '/relay5': { target: 'http://localhost:8091', rewrite: (p: string) => p.replace(/^\/relay5/, '') },
      '/relay6': { target: 'http://localhost:8092', rewrite: (p: string) => p.replace(/^\/relay6/, '') },
      '/spacecraft2/ws': { target: 'ws://localhost:8081', ws: true },
      '/spacecraft3/ws': { target: 'ws://localhost:8081', ws: true },
      '/query-health-ai': 'http://localhost:8080',
    },
  },
})
