import fs from "node:fs";
import path from "node:path";

const diagramsDir = path.dirname(new URL(import.meta.url).pathname);
const demoRoot = path.resolve(diagramsDir, "..");
const repoRoot = path.resolve(demoRoot, "..", "..");
// The workbook and its exported images live in the obsidian vault, not this
// directory — see obsidian/V3-Platform/Architecture/Dictionary-POC/ARCHITECTURE-COMMUNICATIONS.md.
const vaultDir = path.join(repoRoot, "obsidian", "V3-Platform", "Architecture", "Dictionary-POC");
const workbookPath = path.join(vaultDir, "architecture-dictionary.drawio");
const iconsPath = path.resolve(repoRoot, "shared", "unifi-theme", "icons.svg");
const fontStack = "Inter, -apple-system, 'Segoe UI', sans-serif";

const iconSource = fs.readFileSync(iconsPath, "utf8");
const icons = new Map();
for (const match of iconSource.matchAll(/<symbol\b([^>]*)>([\s\S]*?)<\/symbol>/g)) {
  const attributes = match[1];
  const id = attributes.match(/\bid="([^"]+)"/)[1];
  const viewBox = attributes.match(/\bviewBox="([^"]+)"/)[1];
  const rootAttributes = attributes
    .replace(/\s+id="[^"]+"/, "")
    .replace(/\s+viewBox="[^"]+"/, "")
    .trim();
  icons.set(
    id,
    `<svg xmlns="http://www.w3.org/2000/svg" viewBox="${viewBox}" ${rootAttributes} color="#fff">${match[2]}</svg>`,
  );
}

const iconCells = {
  "shipping-ui-map": [
    ["map-app", "ui-lane", "ico-browser", 74, 120],
    ["map-fleet", "ui-lane", "ico-browser", 266, 120],
    ["map-ships", "ui-lane", "ico-browser", 458, 120],
    ["map-terminal", "ui-lane", "ico-browser", 266, 358],
    ["map-l10n", "service-lane", "ico-kv", 94, 122],
    ["map-status", "service-lane", "ico-kv", 334, 122],
    ["map-registry", "service-lane", "ico-db", 574, 122],
    ["map-fallback", "service-lane", "ico-cache", 94, 380],
    ["map-sse", "service-lane", "ico-sse", 334, 380],
  ],
  "localized-rendering": [
    ["lifecycle-seed", "seed", "ico-db", 72, 102],
    ["lifecycle-build", "build", "ico-cache", 72, 102],
    ["lifecycle-port", "port", "ico-browser", 72, 102],
    ["lifecycle-client", "client", "ico-kv", 99, 102],
    ["lifecycle-read", "read", "ico-cache", 118, 102],
    ["lifecycle-sse", "client", "ico-sse", 99, 388],
  ],
  "shipping-ui-sequence": [
    ["sequence-port", "1", "ico-browser", 100, 120],
    ["sequence-composable", "1", "ico-cache", 342, 120],
    ["sequence-backend", "1", "ico-service", 584, 120],
    ["sequence-kv", "1", "ico-nats", 826, 120],
    ["sequence-service", "1", "ico-service", 1068, 120],
    ["sequence-postgres", "1", "ico-db", 1310, 120],
    ["sequence-sse", "1", "ico-sse", 1552, 120],
  ],
  // x/y are absolute page coordinates: node.x + 18, node.y + 8. Keep these in
  // sync with the matching mxGeometry in the workbook whenever a node moves.
  "docker-compose-network": [
    ["net-admin", "1", "ico-container", 268, 528],
    ["net-seafreight", "1", "ico-container", 268, 808],
    ["net-refdata-node", "1", "ico-container", 268, 1088],
    ["net-ship-svc", "1", "ico-container", 848, 508],
    ["net-refdata-svc", "1", "ico-container", 848, 683],
    ["net-accounts-svc", "1", "ico-container", 848, 858],
    ["net-pricing-svc", "1", "ico-container", 848, 1033],
    ["net-tp-svc", "1", "ico-container", 848, 1208],
    ["net-nats", "1", "ico-nats", 1218, 358],
    ["net-postgres", "1", "ico-db", 1218, 523],
    ["net-refdata-pg", "1", "ico-db", 1218, 698],
    ["net-accounts-pg", "1", "ico-db", 1218, 873],
    ["net-pricing-pg", "1", "ico-db", 1218, 1048],
    ["net-tp-pg", "1", "ico-db", 1218, 1230],
    ["net-nats-data", "1", "ico-volume", 1508, 360],
    ["net-nats-logs", "1", "ico-volume", 1508, 417],
    ["net-pg-data", "1", "ico-volume", 1508, 541],
    ["net-refdata-pg-data", "1", "ico-volume", 1508, 716],
    ["net-accounts-pg-data", "1", "ico-volume", 1508, 891],
    ["net-pricing-pg-data", "1", "ico-volume", 1508, 1066],
    ["net-tp-pg-data", "1", "ico-volume", 1508, 1248],
    ["net-creds-vol", "1", "ico-volume", 1508, 1308],
    ["net-nats-ui", "1", "ico-container", 848, 1438],
    ["net-nui", "1", "ico-container", 1073, 1438],
    ["net-nats-tower", "1", "ico-container", 1298, 1438],
    ["net-nui-data", "1", "ico-volume", 1073, 1567],
    ["net-tower-data", "1", "ico-volume", 1298, 1567],
  ],
  "jwt-minting-sequence": [
    ["jwt-nats-icon", "1", "ico-nats", 1318, 168],
    ["jwt-ship-icon", "1", "ico-service", 678, 178],
    ["jwt-refdata-icon", "1", "ico-service", 678, 328],
    ["jwt-accounts-icon", "1", "ico-service", 678, 478],
    ["jwt-pg-icon", "1", "ico-db", 1018, 548],
    ["jwt-creds-icon", "1", "ico-volume", 1018, 438],
  ],
  "rpc-proposed": [
    ["rest-adapter", "1", "ico-service", 130, 222],
    ["natsrpc-adapter", "1", "ico-nats", 480, 222],
    ["commands-node", "1", "ico-service", 250, 452],
    ["domain-node", "1", "ico-service", 250, 612],
    ["store-node", "1", "ico-db", 480, 612],
    ["ship-rpc-client", "1", "ico-nats", 1180, 222],
    ["ship-commands", "1", "ico-service", 1180, 452],
  ],
};

const textCells = [
  "app", "fleet", "ships", "terminal", "locale", "l10n", "status", "registry", "fallback", "sse",
  "seed-node", "build-node", "port-node", "client-node", "read-node",
  "port", "composable", "backend", "kv", "service", "postgres",
  "rest-adapter", "natsrpc-adapter", "commands-node", "domain-node", "store-node",
  "ship-rpc-client", "ship-commands",
  // docker-compose-network: every tiled node carries its glyph at the top-left
  // inset, so its text block is bottom-aligned to clear it (the longer names —
  // trading-partner-service/-postgres — collide with the glyph otherwise).
  "admin-node", "seafreight-node", "refdata-node",
  "ship-svc", "refdata-svc", "accounts-svc", "pricing-svc", "tp-svc",
  "nats", "refdata-pg", "accounts-pg", "pricing-pg", "tp-pg",
  "nats-ui", "nui", "nats-tower",
];

let workbook = fs.readFileSync(workbookPath, "utf8");
workbook = workbook.replaceAll("fontFamily=Inter;", `fontFamily=${fontStack};`);
workbook = workbook.replaceAll("verticalAlign=bottom;spacingBottom=10;", "");
workbook = workbook.replace(/        <mxCell id="unifi-icon-[\s\S]*?<\/mxCell>\n/g, "");

for (const cellId of textCells) {
  const cellPattern = new RegExp(`(<mxCell id="${cellId}"[^>]*style="[^"]*)"`, "g");
  workbook = workbook.replace(cellPattern, "$1verticalAlign=bottom;spacingBottom=10;\"");
}

for (const [pageId, cells] of Object.entries(iconCells)) {
  const pagePattern = new RegExp(`(<diagram id="${pageId}"[\\s\\S]*?<root>)([\\s\\S]*?)(\\s*</root>)`);
  const pageMatch = workbook.match(pagePattern);
  if (!pageMatch) throw new Error(`Page not found: ${pageId}`);

  const renderedCells = cells.map(([name, parent, iconId, x, y]) => {
    const icon = icons.get(iconId);
    if (!icon) throw new Error(`Icon not found: ${iconId}`);
    const data = encodeURIComponent(icon);
    return `        <mxCell id="unifi-icon-${name}" value="" style="shape=image;image=data:image/svg+xml,${data};aspect=fixed;perimeter=0;strokeColor=none;fillColor=none;" vertex="1" parent="${parent}"><mxGeometry x="${x}" y="${y}" width="28" height="28" as="geometry"/></mxCell>`;
  }).join("\n");

  workbook = workbook.replace(pagePattern, `$1$2\n${renderedCells}$3`);
}

fs.writeFileSync(workbookPath, workbook);
