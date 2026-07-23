// Dev-server override for verifying refdata against the *dockerized*
// refdata-service (host port 7201) while docker's own refdata holds
// 7102. Used via `npm run dev -- --config <this file>` from the app dir.
import { defineConfig, mergeConfig } from 'vite'

import baseConfig from './vite.config.js'

export default mergeConfig(
  baseConfig,
  defineConfig({
    server: {
      port: 7103,
      strictPort: true,
      proxy: {
        '/api': {
          target: 'http://localhost:7201',
          changeOrigin: true,
        },
      },
    },
  }),
)
