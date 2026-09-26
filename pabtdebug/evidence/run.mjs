/*
   The executable Chromium verification for the pabtdebug browser client.

   What this is for: a layout claim is a claim about pixels, so it has to be
   measured in a browser, and a claim about what a page did with untrusted text
   has to be measured in a browser too. Everything here reports what it observed
   and fails on a threshold, because a screenshot nobody checked is a claim.

   What it deliberately does not do: re-prove the store's or the hub's
   guarantees. An exact resume with no duplicate and no gap, and a client that
   falls behind being handed a position to resume from, are properties of the
   transport and are covered by the package's own tests, which can control the
   timing a browser cannot. What the browser adds is the consequence: after a
   reload the page must show the same head with no duplicate and nothing missing.

   Teardown is the other half of the job. The target is stopped by its exact pid
   on every path, the listener is confirmed free rather than assumed free, and the
   capability file is confirmed gone - a failed run must not leave a way back into
   a history.
*/

import { chromium } from "playwright";
import { spawn, spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import {
  existsSync, mkdirSync, mkdtempSync, readdirSync, readFileSync, rmSync, statSync, writeFileSync,
} from "node:fs";
import { createConnection } from "node:net";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = fileURLToPath(new URL(".", import.meta.url));
const repoRoot = resolve(here, "../..");

/** The viewports the acceptance names, with what each is for. */
const VIEWPORTS = [
  { name: "desktop", width: 1200, height: 762, why: "the wide default" },
  { name: "laptop", width: 1024, height: 762, why: "the intermediate width" },
  { name: "narrow", width: 700, height: 700, why: "the narrow layout, where the old page overflowed" },
];

/** The minimum heights the acceptance names, in CSS pixels. */
const MINIMUM = { detail: 180, canvas: 150, scrub: 24 };

/** The regions that must not overlap, in the order the acceptance names them. */
const REGIONS = ["header", "breadcrumb", "timeline", "canvas", "detail", "scrub"];

/** Values the target plants, which must never appear in the page. */
const PLANTED_SECRETS = ["hunter2-planted", "planted-capability-value"];

const TICKS = 8;
const TICK_INTERVAL_MS = 250;

function parseArgs(argv) {
  const options = { out: join(repoRoot, "scratch", "evidence"), keep: false };
  for (let i = 0; i < argv.length; i += 1) {
    if (argv[i] === "-out") options.out = resolve(argv[++i]);
    else if (argv[i] === "-keep") options.keep = true;
    else throw new Error(`unknown flag ${argv[i]}`);
  }
  return options;
}

/**
 * buildTarget compiles the evidence target from the current source.
 *
 * The runner starts the binary it just built rather than one found on the path,
 * because "the current binary" is the whole claim: a run against a stale build
 * would measure the previous increment and say so with a straight face.
 */
function buildTarget(outDir) {
  const binary = join(outDir, "durable-target");
  const result = spawnSync("go", ["build", "-o", binary, "./pabtdebug/evidence/target"], {
    cwd: repoRoot,
    encoding: "utf8",
  });
  if (result.status !== 0) {
    throw new Error(`build the target: ${result.stderr || result.stdout}`);
  }
  return binary;
}

/** waitFor polls until check passes or the deadline passes. */
async function waitFor(what, check, timeoutMs = 15000) {
  const deadline = Date.now() + timeoutMs;
  for (;;) {
    try {
      const value = await check();
      if (value) return value;
    } catch {
      // Not ready yet. The deadline is what decides, not this.
    }
    if (Date.now() > deadline) throw new Error(`timed out waiting for ${what}`);
    await new Promise((resolve) => setTimeout(resolve, 50));
  }
}

/** sha256 digests a buffer, for the manifest. */
function digest(bytes) {
  return createHash("sha256").update(bytes).digest("hex");
}

/**
 * portFree reports whether anything is still listening on the target's port.
 *
 * Confirmed rather than assumed. A teardown that returned as soon as the process
 * was signalled would report success while the socket was still open, and the
 * next run would fail to bind for a reason that looks like a bug in the target.
 */
function portFree(address) {
  const port = Number(address.slice(address.lastIndexOf(":") + 1));
  return new Promise((settle) => {
    const probe = createConnection({ host: "127.0.0.1", port });
    const finish = (free) => {
      probe.destroy();
      settle(free);
    };
    probe.once("connect", () => finish(false));
    probe.once("error", () => finish(true));
    setTimeout(() => finish(true), 1500);
  });
}

async function main() {
  const options = parseArgs(process.argv.slice(2));
  mkdirSync(options.out, { recursive: true });
  const workDir = mkdtempSync(join(tmpdir(), "pabtdebug-evidence-"));
  const binary = buildTarget(options.out);
  const findings = [];
  const fail = (message) => {
    findings.push({ level: "FAIL", message });
  };
  const note = (message) => {
    findings.push({ level: "INFO", message });
  };

  const storePath = join(workDir, "history.db");
  const readyFile = join(workDir, "ready");
  const capabilityFile = join(workDir, "capability");
  const logFile = join(options.out, "target.log");
  const log = [];
  let target = null;
  let browser = null;
  // Hoisted so the teardown can check the port the target actually bound, which is
  // the only address worth confirming is free afterwards.
  let address = "";

  try {
    target = spawn(binary, [
      "-store", storePath,
      "-ready-file", readyFile,
      "-capability-file", capabilityFile,
      "-ticks", String(TICKS),
      "-tick-interval", `${TICK_INTERVAL_MS}ms`,
    ], { stdio: ["ignore", "pipe", "pipe"] });
    const record = (stream, prefix) => {
      stream.setEncoding("utf8");
      stream.on("data", (chunk) => {
        for (const line of chunk.split("\n")) {
          if (line.trim() === "") continue;
          log.push(`${prefix}${line}`);
        }
      });
    };
    record(target.stdout, "out: ");
    record(target.stderr, "err: ");

    address = await waitFor("the target to publish its address", () => {
      if (!existsSync(readyFile)) return null;
      const value = readFileSync(readyFile, "utf8").trim();
      return value.includes(":") ? value : null;
    });
    const capability = await waitFor("the target to publish its capability", () => {
      if (!existsSync(capabilityFile)) return null;
      const value = readFileSync(capabilityFile, "utf8").trim();
      return value.length > 32 ? value : null;
    });
    note(`target ${address}, capability ${capability.length} characters, not logged`);

    // The run's own records are waited for through the store, not by counting
    // lines of the target's output: the head is the fact, and a line count would
    // be a second way to be wrong about it.
    await waitFor(`the run to reach tick ${TICKS}`, async () => {
      const response = await fetch(`http://${address}/debug/pabt/v2/state`, {
        headers: { "X-PABT-Capability": capability },
      });
      if (!response.ok) return false;
      const state = await response.json();
      return state.head.HeadSequence >= TICKS ? state : false;
    }, 30000);

    browser = await chromium.launch();
    const context = await browser.newContext();
    const consoleLog = [];
    const network = [];
    const page = await context.newPage();
    page.on("console", (message) => {
      consoleLog.push({ type: message.type(), text: message.text() });
    });
    page.on("pageerror", (error) => {
      consoleLog.push({ type: "pageerror", text: error.message });
    });
    page.on("response", (response) => {
      const status = response.status();
      const entry = {
        method: response.request().method(),
        url: response.url(),
        status,
      };
      network.push(entry);
      if (status < 400) return;
      // The body of a refusal is the server's own account of why, and "404 page
      // not found" says the request never reached a handler while a description
      // says it reached one and was refused. That difference is the diagnosis, so
      // it is captured rather than inferred from the status.
      response.text().then((body) => {
        entry.body = body.slice(0, 300);
      }).catch(() => {});
    });
    page.on("requestfailed", (request) => {
      network.push({
        method: request.method(),
        url: request.url(),
        status: null,
        failed: request.failure()?.errorText ?? "unknown",
      });
    });

    const base = `http://${address}/debug/pabt/ui`;
    const measurements = {};

    for (const viewport of VIEWPORTS) {
      await page.setViewportSize({ width: viewport.width, height: viewport.height });
      await page.goto(base, { waitUntil: "domcontentloaded" });
      // The same context is reused across viewports, so the token is already in
      // this tab's sessionStorage and the page reconnects on its own. Whether it
      // did is decided by waiting for one outcome or the other rather than by
      // asking whether the connect screen is up: asking races the page's own
      // boot, and a run that fills a field the page has just hidden fails for a
      // reason that has nothing to do with the debugger.
      const reconnected = await page
        .waitForSelector("#app:not([hidden])", { timeout: 5000 })
        .then(() => true)
        .catch(() => false);
      if (!reconnected) {
        await page.fill("#capability", capability);
        await page.click("#connect-form button[type=submit]");
      }
      await page.waitForSelector("#app:not([hidden])", { timeout: 15000 });
      await waitFor("the page to show the head", async () => {
        const head = await page.textContent("#identity-head");
        return head && Number(head) >= TICKS;
      }, 15000);
      // The stream is live, so a frame has to land before the page is read.
      await page.waitForTimeout(400);

      // Measured from the top of the document. A page that opens scrolled is
      // broken in a way no rectangle would otherwise reveal, and every y would be
      // measured against the wrong origin.
      await page.evaluate(() => window.scrollTo(0, 0));
      const measured = await page.evaluate((regions) => {
        const rect = (id) => {
          const node = document.getElementById(id);
          const box = node.getBoundingClientRect();
          return { x: box.x, y: box.y, width: box.width, height: box.height };
        };
        const scrub = document.getElementById("scrub-range");
        return {
          scrollWidth: document.documentElement.scrollWidth,
          clientWidth: document.documentElement.clientWidth,
          scrollY: window.scrollY,
          scrollHeight: document.documentElement.scrollHeight,
          clientHeight: document.documentElement.clientHeight,
          rects: Object.fromEntries(regions.map((id) => [id, rect(id)])),
          scrub: {
            tag: scrub.tagName,
            type: scrub.type,
            min: scrub.min,
            max: scrub.max,
            value: scrub.value,
            ariaLabel: scrub.getAttribute("aria-label"),
            ariaValueText: scrub.getAttribute("aria-valuetext"),
            tabbable: scrub.tabIndex >= 0,
          },
          tree: {
            items: document.querySelectorAll('[role="treeitem"]').length,
            selected: document.querySelectorAll('[role="treeitem"][aria-selected="true"]').length,
            groups: document.querySelectorAll('[role="group"]').length,
            expanded: document.querySelectorAll('[role="treeitem"][aria-expanded]').length,
            tabStops: document.querySelectorAll('#tree [role="treeitem"] .node[tabindex="0"]').length,
          },
          evidence: [...document.querySelectorAll("#detail-body .evidence")].map((block) => ({
            heading: block.closest(".field")?.querySelector("h3 span")?.textContent ?? "",
            rows: [...block.querySelectorAll(".ev")].map((row) => ({
              label: row.querySelector(".ev-label")?.textContent ?? "",
              value: row.querySelector(".ev-value")?.textContent ?? "",
              availability: row.querySelector(".ev-value")?.dataset.availability ?? "",
            })),
          })),
          timeline: [...document.querySelectorAll("#timeline-list button")].map((node) => node.dataset.sequence),
          connection: document.getElementById("connection").textContent,
          breadcrumb: document.getElementById("breadcrumb-list").textContent,
          pwned: typeof window.__pabtPwned,
          secretInDom: document.body.innerText.includes("hunter2-planted"),
          images: document.querySelectorAll("img").length,
        };
      }, REGIONS);
      measurements[viewport.name] = { viewport, ...measured };

      // Full page first, then a crop per region, so a reader has both the whole
      // layout and the part that is being talked about.
      await page.screenshot({ path: join(options.out, `page-${viewport.name}.png`), fullPage: true });
      for (const id of REGIONS) {
        const box = measured.rects[id];
        if (!box || box.width < 1 || box.height < 1) continue;
        await page.screenshot({
          path: join(options.out, `crop-${viewport.name}-${id}.png`),
          clip: {
            x: Math.max(0, box.x),
            y: Math.max(0, box.y),
            width: Math.min(box.width, viewport.width - Math.max(0, box.x)),
            height: Math.min(box.height, viewport.height - Math.max(0, box.y)),
          },
        });
      }

      checkLayout(viewport, measured, fail);
    }

    // The evidence panel is checked by walking the whole tree and reading the
    // panel at every occurrence, not by reading the one the page happened to
    // select. A precondition belongs to the node that evaluates it, so asserting
    // on an arbitrary selection would assert on whatever the default happened to
    // be - and would pass or fail for the wrong reason.
    await page.setViewportSize({ width: 1200, height: 762 });
    const walked = await walkTree(page, options.out);
    note(`walked ${walked.occurrences} occurrences and read ${walked.panels.length} panels`);
    checkEvidenceFields(walked.panels, fail, note);
    measurements.walk = {
      occurrences: walked.occurrences,
      panels: walked.panels,
      untrusted: walked.untrusted,
    };

    // A reload is a reconnect with a real position: the page has to rebuild from
    // the store and arrive at the same head with nothing duplicated.
    const before = measurements.desktop.timeline;
    await page.reload({ waitUntil: "domcontentloaded" });
    // A reload keeps the token in sessionStorage, so the page must come back on
    // its own. A short wait proves it did rather than assuming it.
    const cameBack = await page
      .waitForSelector("#app:not([hidden])", { timeout: 5000 })
      .then(() => true)
      .catch(() => false);
    if (!cameBack) fail("a reload did not resume the client from its stored capability");
    const after = await page.evaluate(() =>
      [...document.querySelectorAll("#timeline-list button")].map((n) => n.dataset.sequence));
    if (JSON.stringify(before) !== JSON.stringify(after)) {
      fail(`a reload changed the tick list: ${before.join(",")} became ${after.join(",")}`);
    } else {
      note(`a reload resumed at the same ${after.length} ticks with no duplicate and no gap`);
    }

    // The scrub strip is a control, so it is operated rather than measured.
    const scrubResult = await page.evaluate(() => {
      const range = document.getElementById("scrub-range");
      const first = range.min;
      range.value = String(Number(range.max) - 1);
      range.dispatchEvent(new Event("input", { bubbles: true }));
      return { moved: document.getElementById("scrub-readout").textContent, from: first };
    });
    await page.waitForTimeout(250);
    const movedHead = await page.evaluate(() =>
      [...document.querySelectorAll('#timeline-list button[aria-current="true"]')].map((n) => n.dataset.sequence));
    if (!movedHead.includes(scrubResult.moved)) {
      fail(`the scrub strip moved to ${scrubResult.moved} but selected ${movedHead.join(",") || "nothing"}`);
    } else {
      note(`the scrub strip selected tick ${scrubResult.moved} from ${scrubResult.from}`);
    }
    await page.screenshot({ path: join(options.out, "page-scrubbed.png"), fullPage: true });

    // Untrusted text must have been shown, not run.
    const untrusted = walked.untrusted;
    if (untrusted.pwned !== "undefined") {
      fail(`recorded text executed: window.__pabtPwned is ${untrusted.pwned}`);
    }
    if (untrusted.images !== 0) {
      fail(`recorded text produced ${untrusted.images} elements in the detail panel`);
    }
    if (!untrusted.showsKey || !untrusted.showsTag) {
      fail(`the recorded key is never shown as characters: key=${untrusted.showsKey} tag=${untrusted.showsTag}`);
    } else {
      note("the recorded key is displayed as the characters it is, and created no element");
    }

    // Nothing secret may be in anything the run captured.
    const errors = consoleLog.filter((entry) => entry.type === "error" || entry.type === "pageerror");
    if (errors.length > 0) {
      fail(`the page reported ${errors.length} console errors: ${errors.map((e) => e.text).join(" | ")}`);
    }
    for (const secret of PLANTED_SECRETS) {
      if (measurements.desktop.secretInDom) fail("a planted secret is in the page text");
      const inConsole = consoleLog.some((entry) => entry.text.includes(secret));
      if (inConsole) fail(`a planted secret is in the console: ${secret}`);
      const inNetwork = network.some((entry) => entry.url.includes(secret));
      if (inNetwork) fail(`a planted secret is in a request URL: ${secret}`);
      const inLog = log.some((line) => line.includes(secret));
      if (inLog) fail(`a planted secret is in the target's output: ${secret}`);
    }
    // A capability in a URL is a capability in an access log.
    for (const entry of network) {
      if (entry.url.includes(capability)) {
        fail(`the capability appears in a request URL: ${entry.url}`);
      }
    }
    const withToken = network.filter((entry) => entry.url.includes("capability=") && !entry.url.includes("cursor="));
    if (withToken.length > 0) {
      fail(`a capability query parameter was sent: ${withToken[0].url}`);
    }
    note(`${network.length} requests observed, none carrying the capability in a URL`);

    // The transfer controls, exercised through the page rather than around it.
    // The bundle is what leaves the machine, so it is scanned for the same
    // secrets the page was scanned for: a redaction that holds in the record and
    // not in the export is a redaction that does not hold.
    const [download] = await Promise.all([
      page.waitForEvent("download", { timeout: 20000 }),
      page.click("#export"),
    ]);
    const bundlePath = await download.path();
    const bundle = readFileSync(bundlePath);
    const bundleText = bundle.toString("latin1");
    for (const secret of PLANTED_SECRETS) {
      if (bundleText.includes(secret)) {
        fail(`a planted secret is in the exported bundle: ${secret}`);
      }
    }
    if (!bundleText.includes("REDACTED")) {
      fail("the exported bundle carries no redaction marker, so nothing was redacted on the way out");
    }
    note(`the exported bundle is ${bundle.length} bytes and carries no planted secret`);

    // The import control, given the bundle the export control just produced.
    const generationBefore = await page.textContent("#identity-generation");
    await page.setInputFiles("#import", {
      name: "evidence-run.pabt",
      mimeType: "application/vnd.pabtdebug.bundle",
      buffer: bundle,
    });
    const republished = await waitFor("the import to publish a new generation", async () => {
      const shown = await page.textContent("#identity-generation");
      return shown && shown !== generationBefore ? shown : false;
    }, 20000);
    note(`the import republished the store at generation ${republished}`);
    await page.screenshot({ path: join(options.out, "page-imported.png"), fullPage: true });
    const afterImport = await page.evaluate(() =>
      [...document.querySelectorAll("#timeline-list button")].map((n) => n.dataset.sequence));
    if (afterImport.length !== TICKS) {
      fail(`after the import the page shows ${afterImport.length} ticks, want ${TICKS}`);
    } else {
      note(`after the import the page shows the same ${TICKS} ticks, from the imported history`);
    }

    const digestValue = await runDigest(address, capability);
    note(`run shape digest ${digestValue}`);

    const dom = await page.evaluate(() => document.documentElement.outerHTML);
    writeFileSync(join(options.out, "dom.html"), dom);
    writeFileSync(join(options.out, "console.json"), JSON.stringify(consoleLog, null, 2));
    writeFileSync(join(options.out, "network.json"), JSON.stringify(network, null, 2));
    writeFileSync(join(options.out, "measurements.json"), JSON.stringify(measurements, null, 2));
    writeFileSync(join(options.out, "findings.json"), JSON.stringify(findings, null, 2));
    writeFileSync(logFile, log.join("\n") + "\n");

    const manifest = buildManifest(options.out, digestValue);
    writeFileSync(join(options.out, "manifest.json"), JSON.stringify(manifest, null, 2));
    writeFileSync(join(options.out, "dom.sha256"), digest(Buffer.from(dom)) + "  dom.html\n");

    for (const entry of findings) {
      console.log(`${entry.level}: ${entry.message}`);
    }
    if (findings.some((entry) => entry.level === "FAIL")) {
      process.exitCode = 1;
      console.log(`\nevidence written to ${options.out}`);
      return;
    }
    console.log(`\nevidence written to ${options.out}`);
  } finally {
    if (browser) await browser.close().catch(() => {});
    await stopTarget(target, address, capabilityFile, fail);
    if (!options.keep) rmSync(workDir, { recursive: true, force: true });
    else note(`kept the work directory ${workDir}`);
    const failed = findings.some((entry) => entry.level === "FAIL");
    if (failed) {
      writeFileSync(join(options.out, "findings.json"), JSON.stringify(findings, null, 2));
      process.exitCode = 1;
    }
  }
}

/** checkLayout applies the acceptance criteria to one viewport's measurements. */
function checkLayout(viewport, measured, fail) {
  const where = `${viewport.name} ${viewport.width}x${viewport.height}`;
  if (measured.scrollWidth > measured.clientWidth) {
    fail(`${where}: the document scrolls sideways, ${measured.scrollWidth} wide against ${measured.clientWidth}`);
  }
  if (measured.scrollY !== 0) {
    fail(`${where}: the page is scrolled to ${measured.scrollY}, so every region is measured from the wrong origin`);
  }
  if (measured.scrollHeight > measured.clientHeight + 1) {
    fail(`${where}: the document is ${measured.scrollHeight} tall against a viewport of ${measured.clientHeight}, ` +
      "so it can be scrolled off its own content");
  }
  for (const id of REGIONS) {
    const box = measured.rects[id];
    if (box.y < -0.5) {
      fail(`${where}: ${id} starts above the viewport at y=${Math.round(box.y)}`);
    }
    if (box.x < -0.5) {
      fail(`${where}: ${id} starts left of the viewport at x=${Math.round(box.x)}`);
    }
  }
  for (const [id, minimum] of Object.entries(MINIMUM)) {
    const height = measured.rects[id].height;
    if (height < minimum) {
      fail(`${where}: ${id} is ${Math.round(height)}px tall, the floor is ${minimum}px`);
    }
  }
  // Overlap is checked pairwise against the rectangles the browser resolved, so
  // it is the layout that is measured rather than the stylesheet that is read.
  for (let i = 0; i < REGIONS.length; i += 1) {
    for (let j = i + 1; j < REGIONS.length; j += 1) {
      const a = measured.rects[REGIONS[i]];
      const b = measured.rects[REGIONS[j]];
      const overlapX = Math.min(a.x + a.width, b.x + b.width) - Math.max(a.x, b.x);
      const overlapY = Math.min(a.y + a.height, b.y + b.height) - Math.max(a.y, b.y);
      if (overlapX > 0.5 && overlapY > 0.5) {
        fail(`${where}: ${REGIONS[i]} and ${REGIONS[j]} overlap by ` +
          `${Math.round(overlapX)}x${Math.round(overlapY)} pixels`);
      }
    }
  }
  if (measured.scrub.tag !== "INPUT" || measured.scrub.type !== "range") {
    fail(`${where}: the scrub strip is a ${measured.scrub.tag}, not a range input`);
  }
  if (!measured.scrub.ariaLabel || !measured.scrub.ariaValueText) {
    fail(`${where}: the scrub strip has no accessible name or value text`);
  }
  if (!measured.scrub.tabbable) {
    fail(`${where}: the scrub strip cannot be reached by keyboard`);
  }
  if (Number(measured.scrub.max) < Number(measured.scrub.min)) {
    fail(`${where}: the scrub strip's range is inverted`);
  }
  if (measured.tree.items === 0) {
    fail(`${where}: the tree drew no occurrences`);
  }
  if (measured.tree.tabStops !== 1) {
    fail(`${where}: the tree has ${measured.tree.tabStops} tab stops, want exactly 1`);
  }
  const sequences = measured.timeline;
  const unique = new Set(sequences);
  if (unique.size !== sequences.length) {
    fail(`${where}: the tick list repeats a sequence: ${sequences.join(",")}`);
  }
  const sorted = [...sequences].sort((a, b) => Number(a) - Number(b));
  if (JSON.stringify(sorted) !== JSON.stringify(sequences)) {
    fail(`${where}: the tick list is out of order: ${sequences.join(",")}`);
  }
}

/**
 * runDigest digests the shape of the run: every record's sequence, kind, status,
 * presence of a tree, and node count, in order.
 *
 * The store mints a random identity per file and that identity is in every
 * record, so a digest over the records themselves changes on every run by
 * design. This one leaves the identity out, so two runs of the same binary
 * against a fresh store must produce the same value - which is the property that
 * makes the target deterministic rather than merely repeatable. A difference here
 * means the run's shape changed, which is the thing worth knowing.
 */
async function runDigest(address, capability) {
  const response = await fetch(`http://${address}/debug/pabt/v2/records?after=0`, {
    headers: { "X-PABT-Capability": capability },
  });
  if (!response.ok) throw new Error(`read the run: ${response.status}`);
  const { page: recordPage } = await response.json();
  const shape = recordPage.Records.map((record) => [
    record.sequence,
    record.kind,
    record.statusPresent ? record.status : "-",
    record.tree ? record.tree.nodeCount : "-",
  ]);
  return digest(Buffer.from(JSON.stringify(shape), "utf8"));
}

/**
 * walkTree opens every subtree, selects every occurrence, and records the detail
 * panel at each one.
 *
 * Opening everything rather than reading the default selection is the point: a
 * precondition is recorded against the node that evaluates it, so the panel that
 * explains it is only reachable by selecting that node.
 */
async function walkTree(page, outDir) {
  const opened = await page.evaluate(() => {
    // Every collapsed item is opened by clicking its row with shift held, which
    // is the control the page offers.
    let opened = 0;
    for (let pass = 0; pass < 12; pass += 1) {
      const closed = [...document.querySelectorAll('#tree [role="treeitem"][aria-expanded="false"]')];
      if (closed.length === 0) break;
      for (const item of closed) {
        item.querySelector(".node").dispatchEvent(
          new MouseEvent("click", { bubbles: true, shiftKey: true }),
        );
        opened += 1;
      }
    }
    return opened;
  });
  void opened;
  const panels = [];
  let occurrences = 0;
  let capturedUndeclared = false;
  const untrusted = { pwned: "unknown", images: 0, showsKey: false, showsTag: false };
  for (;;) {
    const ids = await page.evaluate(() =>
      [...document.querySelectorAll('#tree [role="treeitem"]')].map((n) => n.dataset.nodeId));
    if (ids.length === 0) break;
    if (occurrences >= ids.length) break;
    for (const id of ids) {
      const present = await page.evaluate((nodeId) => {
        const item = document.querySelector(`#tree [role="treeitem"][data-node-id="${CSS.escape(nodeId)}"]`);
        if (!item) return null;
        item.querySelector(".node").click();
        return true;
      }, id);
      if (!present) continue;
      occurrences += 1;
      const panel = await page.evaluate(() => ({
        nodeId: document.querySelector('#tree [role="treeitem"][aria-selected="true"]')?.dataset.nodeId ?? "",
        breadcrumb: document.getElementById("breadcrumb-list").textContent,
        text: document.getElementById("detail-body").innerText,
        blocks: [...document.querySelectorAll("#detail-body .evidence")].map((block) => ({
          heading: block.closest(".field")?.querySelector("h3 span")?.textContent ?? "",
          rows: [...block.querySelectorAll(".ev")].map((row) => ({
            label: row.querySelector(".ev-label")?.textContent ?? "",
            value: row.querySelector(".ev-value")?.textContent ?? "",
            availability: row.querySelector(".ev-value")?.dataset.availability ?? "",
          })),
        })),
      }));
      panels.push(panel);
      if (panel.text.includes("harbor.manifest.sealed")) untrusted.showsKey = true;
      if (panel.text.includes("<img src=x")) untrusted.showsTag = true;
      // The one panel a reader most needs to see is the one where the application
      // declared nothing, because that is where an inferred expectation would
      // show up as a fact. It is captured rather than described.
      const undeclaredHere = panel.blocks.some((block) => {
        const expected = block.rows.find((row) => row.label === "expected");
        return expected && expected.availability === "unavailable";
      });
      if (undeclaredHere && !capturedUndeclared) {
        capturedUndeclared = true;
        const box = await page.evaluate(() => {
          const region = document.getElementById("detail").getBoundingClientRect();
          return { x: region.x, y: region.y, width: region.width, height: region.height };
        });
        await page.screenshot({ path: join(outDir, "crop-undeclared-expected.png"), clip: box });
      }
    }
    break;
  }
  const globals = await page.evaluate(() => ({
    pwned: typeof window.__pabtPwned,
    images: document.querySelectorAll("img").length,
  }));
  untrusted.pwned = globals.pwned;
  untrusted.images = globals.images;
  return { occurrences, panels, untrusted };
}

/**
 * checkEvidenceFields asserts the four fields are four separately labelled
 * blocks, and that an expected value the application never declared reads as
 * unavailable rather than as a value inferred from the key.
 */
function checkEvidenceFields(panels, fail, note) {
  const blocks = panels.flatMap((panel) => panel.blocks).filter((block) => /precondition/i.test(block.heading));
  if (blocks.length === 0) {
    fail("no occurrence in the tree shows a precondition block");
    return;
  }
  let labelled = true;
  for (const block of blocks) {
    const labels = block.rows.map((row) => row.label);
    for (const wanted of ["variable", "expected", "current", "observed", "uncertainty"]) {
      if (!labels.includes(wanted)) {
        fail(`a precondition block has no separately labelled ${wanted} field: ${labels.join(", ")}`);
        labelled = false;
      }
    }
    if (new Set(labels).size !== labels.length) {
      fail(`a field is labelled twice in a precondition block: ${labels.join(", ")}`);
      labelled = false;
    }
  }
  if (labelled) {
    note(`${blocks.length} precondition blocks each label variable, expected, current, observed, and uncertainty separately`);
  }
  // Somewhere in the run there is a condition that declares nothing, and its
  // expected value must say so. It cannot be filled in from the key.
  const undeclared = blocks.some((block) => {
    const expected = block.rows.find((row) => row.label === "expected");
    return expected && expected.availability === "unavailable" &&
      /declar|no expected|unavailable/i.test(expected.value);
  });
  if (!undeclared) {
    fail("no condition shows an undeclared expected value as unavailable");
  } else {
    note("an undeclared expected value renders as unavailable, with the record's own reason");
  }
  // The declared case must be distinguishable from the undeclared one, or
  // "unavailable" would prove nothing.
  const declared = blocks.some((block) => {
    const expected = block.rows.find((row) => row.label === "expected");
    return expected && expected.availability !== "unavailable" && expected.value !== "";
  });
  if (!declared) {
    fail("no condition shows a declared expected value, so unavailable proves nothing");
  } else {
    note("a declared expected value renders as a value, distinctly from the unavailable case");
  }
}

/** buildManifest digests every artifact, so a run is comparable to the next one. */
function buildManifest(outDir, runShapeDigest) {
  const files = [];
  for (const name of readdirSync(outDir).sort()) {
    if (name === "manifest.json") continue;
    const path = join(outDir, name);
    if (!statSync(path).isFile()) continue;
    files.push({ name, bytes: statSync(path).size, sha256: digest(readFileSync(path)) });
  }
  return {
    tool: "pabtdebug evidence runner",
    playwright: readFileSync(join(here, "node_modules/playwright/package.json"), "utf8")
      .match(/"version":\s*"([^"]+)"/)[1],
    viewports: VIEWPORTS,
    minimums: MINIMUM,
    // Reproducible: the same binary against a fresh store must produce this
    // value. The artifact digests below deliberately are not expected to match
    // between runs, because the store mints a random identity per file and the
    // page shows it.
    runShapeDigest,
    artifacts: files,
  };
}

/**
 * stopTarget ends the target by its exact pid and then proves the teardown three
 * ways: the process is gone, the port is free, and the capability file is gone.
 *
 * All three are checked rather than assumed, because each has a failure mode that
 * looks like something else. A process that ignored the signal looks like a hang,
 * a port still held looks like a bind failure in the next run, and a capability
 * file left behind looks like a working server to anything that finds it.
 */
async function stopTarget(target, address, capabilityFile, fail) {
  if (!target) return;
  if (target.exitCode === null) {
    // The exact pid and nothing else. A pattern match would reach any process on
    // the machine that happened to share a name.
    target.kill("SIGTERM");
    const exited = await waitFor("the target to exit", () => target.exitCode !== null, 8000)
      .then(() => true)
      .catch(() => false);
    if (!exited) {
      fail("the target ignored SIGTERM and had to be killed");
      target.kill("SIGKILL");
      await waitFor("the target to die", () => target.exitCode !== null, 5000).catch(() => {});
    }
  }
  if (address && !(await portFree(address))) {
    fail(`something is still listening on ${address} after the target exited`);
  }
  if (capabilityFile && existsSync(capabilityFile)) {
    fail(`the capability file is still there: ${capabilityFile}`);
  }
}

await main();
