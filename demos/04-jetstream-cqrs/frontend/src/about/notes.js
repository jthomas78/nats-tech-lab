// The demo's own README, turned into HTML for the About panel.
//
// The README is the source on purpose. It is the text the lab shell already
// shows, it is reviewed with the code, and rendering it here means the notes
// on this screen cannot drift from the notes in the repo. Nothing is written
// twice.
//
// The file is ours, compiled in at build time, and never fetched from
// anywhere — so there is no untrusted HTML to sanitise here.
import { marked } from 'marked'

// A README image is written for GitHub, so its src is a path relative to the
// README (`diagrams/cqrs-blocks.png`). The browser cannot follow that. The
// caller passes the URL Vite gave the same file, keyed by the path as written.
export function renderNotes(markdown, assets = {}) {
  return marked
    .parse(markdown)
    .replace(
      /<img([^>]*?)src="([^"]*)"/g,
      (whole, attrs, src) => `<img${attrs}src="${assets[src] ?? src}"`,
    )
}
