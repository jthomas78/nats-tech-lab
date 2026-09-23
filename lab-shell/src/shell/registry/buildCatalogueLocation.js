/* Where the generated catalogue lives, said once.

   The generator (tools/buildCatalogue/, Node) and the client
   (buildCatalogueClient.js, browser) must agree on this string, and they share
   no other module: the generator must not pull in the manifest schema and the
   client must not pull in `node:fs`. A two-constant module with no imports is
   what keeps the agreement a fact rather than a convention.

   Deliberately NOT under `/plugins/`. That prefix belongs to plugin assets
   (BR-AS77, task 16c), and the catalogue is the document that lists them, not
   one of them. */

/** The URL the shell reads, relative to its own origin. */
export const BUILD_CATALOGUE_PATH = '/plugin-catalogue.json'

/** The file name emitted into `dist/`. The same document, at rest. */
export const BUILD_CATALOGUE_ASSET = 'plugin-catalogue.json'
