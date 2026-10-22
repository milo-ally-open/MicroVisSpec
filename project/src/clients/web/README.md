# Web and PWA client

This Vue 3 + TypeScript application is shared by the browser client and the PWA.

## Run locally

```sh
npm install
npm run dev
```

Use `localhost` for local PWA testing. Browsers require a secure context for service workers and installation; deploy the app over HTTPS outside local development.

For production hosting, serve the app from the manifest scope (`/` with the current configuration), rewrite client-side routes to `index.html`, and allow the generated `sw.js` to be fetched from the app root. If deploying under a subpath, update Vite's `base` and the manifest's `start_url` and `scope` together.

By default the Docker Compose port bindings are loopback-only. For deployment, set `WEB_BIND_ADDRESS` to the host address reachable by browser clients. When Atlas reaches the host through a separate interface, also set `WEB_ATLAS_BIND_ADDRESS` and run Compose with `docker-compose.atlas.yaml`. Set `WEB_HTTPS_REDIRECT_PORT` to the public HTTPS port; this may differ from the container host port `WEB_HTTPS_PORT` when Windows/WSL port forwarding is used.

## Check the production PWA

```sh
npm run build
npm run preview
```

Open the preview URL in a supported browser and inspect the Web App Manifest and service worker in browser developer tools. The service worker precaches only the built application shell and static assets. API requests, media streams, and MCP traffic are not cached. Offline use therefore covers the application shell only; it does not make live data available offline.

PWA update notices are handled by the shared app shell. Push notifications, offline data workflows, and platform-specific installation guidance can be added when those product requirements are designed.
