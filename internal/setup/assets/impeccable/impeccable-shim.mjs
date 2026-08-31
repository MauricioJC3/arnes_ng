#!/usr/bin/env node
/**
 * Bridges the arnes hook contract to impeccable's Claude Code hook scripts.
 *
 * arnes gives a hook `{ "tool": "edit_file", "input": {...} }` on stdin and
 * decides PreTool blocking from the exit code. impeccable's scripts expect a
 * Claude Code event `{ tool_name, tool_input, hook_event_name, ... }` and, for
 * PreToolUse, signal a block with `{"permission":"deny"}` on stdout while
 * always exiting 0. This shim translates both directions.
 *
 * Usage:  node impeccable-shim.mjs <pre|post|stop>
 *
 * It never throws out: a shim failure must not break the turn (exit 0, silent),
 * except a genuine impeccable "deny" which becomes exit 1 so arnes cancels the
 * write.
 */
import { spawnSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { homedir } from "node:os";

const SKILL_DIR = join(homedir(), ".arnes", "skills", "impeccable", "scripts");
const phase = (process.argv[2] || "post").toLowerCase();

const TOOL_NAME = {
  edit_file: "Edit",
  write_file: "Write",
  bash: "Bash",
  read_file: "Read",
};

function readStdin() {
  try {
    return readFileSync(0, "utf8");
  } catch {
    return "";
  }
}

function bail(code = 0, msg = "") {
  if (msg) process.stderr.write(msg.endsWith("\n") ? msg : msg + "\n");
  process.exit(code);
}

let arnesEvent;
try {
  arnesEvent = JSON.parse(readStdin() || "{}");
} catch {
  bail(0);
}

const rawInput =
  arnesEvent.input && typeof arnesEvent.input === "object" ? arnesEvent.input : {};
const toolInput = { ...rawInput };
// impeccable keys off tool_input.file_path; arnes edit/write use `path`.
if (typeof toolInput.path === "string" && toolInput.file_path === undefined) {
  toolInput.file_path = toolInput.path;
}

const hookEventName =
  phase === "pre" ? "PreToolUse" : phase === "stop" ? "Stop" : "PostToolUse";

const event = {
  hook_event_name: hookEventName,
  tool_name: TOOL_NAME[arnesEvent.tool] || arnesEvent.tool || null,
  tool_input: toolInput,
  session_id: process.env.ARNES_SESSION_ID || "arnes",
  cwd: process.cwd(),
};

const script = phase === "pre" ? "hook-before-edit.mjs" : "hook.mjs";
const run = spawnSync(process.execPath, [join(SKILL_DIR, script)], {
  input: JSON.stringify(event),
  encoding: "utf8",
  timeout: 25_000,
});

if (run.error || typeof run.stdout !== "string") bail(0);

const out = run.stdout.trim();
if (!out) bail(0);

let parsed = null;
try {
  parsed = JSON.parse(out);
} catch {
  // Non-JSON stdout: for post, pass it through as a note; for pre, ignore.
  if (phase === "post") process.stdout.write(out + "\n");
  bail(0);
}

if (phase === "pre") {
  if (parsed && parsed.permission === "deny") {
    bail(1, parsed.user_message || parsed.reason || "impeccable design hook blocked this write.");
  }
  bail(0);
}

// post / stop: surface impeccable's additionalContext as the hook note.
const note =
  parsed?.hookSpecificOutput?.additionalContext ||
  parsed?.additionalContext ||
  parsed?.systemMessage ||
  (typeof parsed === "string" ? parsed : "");
if (note) process.stdout.write(String(note).trim() + "\n");
bail(0);
