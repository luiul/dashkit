// Window Registry: every VS Code window self-registers into
// ~/.local/state/vscode-windows/ as one small JSON file, rewritten on a
// heartbeat. External tools read that directory to learn which window has
// which folder open, so they can focus the right window by exact path
// instead of parsing window titles. See README.md for why this exists.

const vscode = require("vscode");
const fs = require("fs");
const os = require("os");
const path = require("path");

// Readers drop entries whose mtime is older than 30s, so the heartbeat
// must be well under that. 5s keeps a closed window visible for at most
// ~30s while costing one tiny file write per window per beat.
const HEARTBEAT_MS = 5000;

function registryDir() {
  return path.join(os.homedir(), ".local", "state", "vscode-windows");
}

// vscode.env.sessionId is unique per window instance, so each window owns
// exactly one file and no two windows ever write the same one.
function registryFile() {
  return path.join(registryDir(), vscode.env.sessionId + ".json");
}

function record() {
  const folders = (vscode.workspace.workspaceFolders || []).map(
    (f) => f.uri.fsPath,
  );
  const workspaceFile = vscode.workspace.workspaceFile
    ? vscode.workspace.workspaceFile.fsPath
    : null;
  const payload = JSON.stringify({
    sessionId: vscode.env.sessionId,
    folders: folders,
    workspaceFile: workspaceFile,
    updatedAt: new Date().toISOString(),
  });
  const file = registryFile();
  try {
    fs.mkdirSync(registryDir(), { recursive: true });
    // Write to a temp file, then rename: a reader can never see a
    // half-written JSON document.
    fs.writeFileSync(file + ".tmp", payload);
    fs.renameSync(file + ".tmp", file);
  } catch {
    // A failed write is harmless: the previous file simply goes stale and
    // readers stop trusting this window within 30s.
  }
}

function activate(context) {
  record();
  const timer = setInterval(record, HEARTBEAT_MS);
  context.subscriptions.push(
    { dispose: () => clearInterval(timer) },
    vscode.workspace.onDidChangeWorkspaceFolders(record),
  );
}

function deactivate() {
  // Best effort only: a killed or crashed window never runs this, so
  // readers prune stale entries by mtime instead of trusting deletes.
  try {
    fs.unlinkSync(registryFile());
  } catch {}
}

module.exports = { activate, deactivate };
