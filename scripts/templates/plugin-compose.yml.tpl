  __PLUGIN_ID__-frontend:
    build:
      context: ../..
      dockerfile: lab-shell/plugins/__PLUGIN_ID__/Dockerfile
      additional_contexts:
        mfe-plugin-host: service:mfe-plugin-host
    container_name: lb-__PLUGIN_ID__
    ports:
      - "__PLUGIN_PORT__:8080"
    labels:
      com.nats-tech-lab.mfe.source: announced
    # `backend` only. A plugin container used to join `frontend` as well so
    # the registry could dial its /healthz from inside the network; nothing
    # dials it now (Phase 15c). The browser still reaches it, on the published
    # host port above — which is what it always used, never this network.
    networks:
      - backend
    environment:
      HTTP_ADDR: ":8080"
      ASSET_ROOT: /srv
      ASSET_ALLOWED_ORIGIN: http://localhost:7110
      PLUGIN_PUBLIC_ORIGIN: http://localhost:__PLUGIN_PORT__
      HEALTH_SELF_URL: http://127.0.0.1:8080/healthz
      NATS_URL: nats://nats:4222
      NATS_CREDS_PATH: /etc/nats/creds/plugin.creds
      NATS_CONNECTION_NAME: __PLUGIN_ID__
      PUBLISHER_ID: __PLUGIN_ID__
      PLUGIN_MANIFEST_PATH: /srv/manifest.json
      PUBLISHER_SIGNING_SEED_PATH: /etc/plugin/signing.nk
      RELEASE_STATE_PATH: /var/lib/announcer/release.json
    volumes:
      - ./nats/creds/plugins/__PLUGIN_ID__.creds:/etc/nats/creds/plugin.creds:ro
      - ./nats/keys/publisher-__PLUGIN_ID__.nk:/etc/plugin/signing.nk:ro
      - __PLUGIN_ID__-release:/var/lib/announcer
    stop_grace_period: 30s
    restart: unless-stopped
    depends_on: *plugin_dependencies
