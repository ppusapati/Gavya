// Every screen is a Connect call the browser makes to the gateway itself, and
// the gateway is chosen at runtime rather than baked in at build time — so
// there is nothing for a server to render, and nothing to prerender.
export const ssr = false;
export const prerender = false;
