#!/bin/sh
# Renders all six artboards onto one contact sheet and screenshots it to
# phase2-registry-mockups.png (4112x3750, 2x device scale).
#
# The .dc.html artboards are for the design canvas: they wrap the body in
# <x-dc>/<helmet>, which a plain browser does not understand. So this script
# reassembles each artboard from parts/ with two extra display rules, lays them
# out in an iframe grid at 1920x1080 (the design viewport, scaled 0.5), and
# lets headless Chrome take one shot. Run build.sh first if parts/ changed.
set -e
cd "$(dirname "$0")"
CHROME=${CHROME:-/Applications/Google Chrome.app/Contents/MacOS/Google Chrome}
[ -x "$CHROME" ] || { echo "set CHROME to a Chrome/Chromium binary" >&2; exit 1; }
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

for n in Main ShellSignal EntryEditor AuditTrail StaleRevision OriginRefused; do
  { cat parts/head.html
    echo '<style>x-dc{display:block}helmet{display:none}</style>'
    cat "parts/body-$n.html"
    cat parts/tail.html
  } > "$WORK/r-$n.html"
done

cat > "$WORK/sheet.html" <<'SHEET'
<!doctype html>
<html><head><meta charset="utf-8">
<link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=Inter:wght@400;600;700;800&display=swap">
<style>
  html,body{margin:0;background:#131416;color:#dee0e3;
    font-family:'Inter',-apple-system,'Segoe UI',Roboto,'Helvetica Neue',Arial,sans-serif;
    font-size:13px;line-height:20px;-webkit-font-smoothing:antialiased;}
  .page{width:2056px;padding:48px;box-sizing:border-box;}
  .eyebrow{font-size:11px;font-weight:700;letter-spacing:.10em;text-transform:uppercase;color:#006fff;}
  h1{font-size:26px;line-height:34px;font-weight:700;margin:6px 0 0;}
  .sub{color:#b7bcc2;margin-top:6px;max-width:1200px;}
  .rule{height:1px;background:#2c3138;margin:22px 0 26px;}
  .row{display:flex;gap:40px;margin-bottom:44px;}
  .cell{display:flex;flex-direction:column;}
  .cap{font-size:12px;font-weight:600;color:#dee0e3;margin-bottom:8px;display:flex;gap:8px;align-items:baseline;}
  .cap .n{font-size:10px;font-weight:700;letter-spacing:.08em;text-transform:uppercase;color:#737c87;}
  .frame{border:1px solid #2c3138;border-radius:4px;overflow:hidden;background:#131416;}
  iframe{border:0;display:block;transform-origin:0 0;}
  .big iframe{width:1920px;height:1080px;transform:scale(.5);}
  .big .frame{width:960px;height:540px;}
  .sm iframe{width:640px;height:460px;transform:scale(.75);}
  .sm .frame{width:480px;height:345px;}
  .foot{color:#737c87;font-size:11px;margin-top:4px;}
</style></head>
<body><div class="page">
  <div class="eyebrow">Application shell · Phase 2 — proposed</div>
  <h1>Dynamic platform registry — curation screens</h1>
  <div class="sub">Four screens and the two write refusals the design creates. Curated registry as service state: Postgres source of truth, KV write-through read cache, server-assigned revision as the concurrency token, config-level origin allowlist, audited writes. Nothing active is ever torn down — a removal is offered to a running shell as a reload.</div>
  <div class="rule"></div>

  <div class="row big">
    <div class="cell"><div class="cap"><span class="n">01</span>Frontend Plugins — curated registry</div>
      <div class="frame"><iframe src="r-Main.html" scrolling="no"></iframe></div></div>
    <div class="cell"><div class="cap"><span class="n">02</span>Shell — catalog changed</div>
      <div class="frame"><iframe src="r-ShellSignal.html" scrolling="no"></iframe></div></div>
  </div>
  <div class="row big">
    <div class="cell"><div class="cap"><span class="n">03</span>Entry editor</div>
      <div class="frame"><iframe src="r-EntryEditor.html" scrolling="no"></iframe></div></div>
    <div class="cell"><div class="cap"><span class="n">04</span>Registry audit</div>
      <div class="frame"><iframe src="r-AuditTrail.html" scrolling="no"></iframe></div></div>
  </div>
  <div class="row sm">
    <div class="cell"><div class="cap"><span class="n">05</span>Refused — stale revision <span class="n">BR-AS18</span></div>
      <div class="frame"><iframe src="r-StaleRevision.html" scrolling="no"></iframe></div>
      <div class="foot">Optimistic concurrency. Refuses; never merges.</div></div>
    <div class="cell"><div class="cap"><span class="n">06</span>Refused — origin not allowlisted <span class="n">BR-AS20</span></div>
      <div class="frame"><iframe src="r-OriginRefused.html" scrolling="no"></iframe></div>
      <div class="foot">Config-level allowlist, enforced on write and on read.</div></div>
  </div>
</div></body></html>
SHEET

"$CHROME" --headless=new --disable-gpu --hide-scrollbars --allow-file-access-from-files \
  --force-device-scale-factor=2 --window-size=2056,1875 --default-background-color=131416FF \
  --virtual-time-budget=8000 \
  --screenshot="$PWD/phase2-registry-mockups.png" "file://$WORK/sheet.html" 2>/dev/null
echo "wrote phase2-registry-mockups.png"
