// Dev-server override for verifying refdata against the *dockerized*
// refdata-service (host port 18081) while docker's own refdata holds
// 5175. Used via `npm run dev -- --config <this file>` from the app dir.
import { defineConfig, mergeConfig } from 'vite'

import baseConfig from './vite.config.js'

export default mergeConfig(
  baseConfig,
  defineConfig({
    server: {
      port: 5199,
      strictPort: true,
      proxy: {
        '/api': {
          target: 'http://localhost:18081',
          changeOrigin: true,
        },
      },
    },
  }),
)
