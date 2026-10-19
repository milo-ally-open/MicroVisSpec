// Navigation fallback is limited to the app shell. These routes must never
// receive an offline HTML response, and no runtime cache is registered.
export const pwaNavigationDenylist = [
  /^\/api\//,
  /^\/evidence\//,
  /^\/media\//,
  /^\/push\//,
]
