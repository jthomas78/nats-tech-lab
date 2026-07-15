import fs from "node:fs";
import path from "node:path";

const diagramsDir = path.dirname(new URL(import.meta.url).pathname);
const demoRoot = path.resolve(diagramsDir, "..");
const workbookPath = path.join(diagramsDir, "architecture-dictionary.drawio");
const iconsPath = path.resolve(demoRoot, "..", "..", "shared", "unifi-theme", "icons.svg");
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
    ["map-uicopy", "service-lane", "ico-kv", 94, 122],
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
};

const textCells = [
  "app", "fleet", "ships", "terminal", "locale", "uicopy", "status", "registry", "fallback", "sse",
  "seed-node", "build-node", "port-node", "client-node", "read-node",
  "port", "composable", "backend", "kv", "service", "postgres",
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
