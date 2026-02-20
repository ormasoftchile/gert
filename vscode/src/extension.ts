import * as vscode from 'vscode';
import * as YAML from 'yaml';
import * as path from 'path';
import * as fs from 'fs';
import { exec } from 'child_process';
import { validateRunbook, ValidationError } from './schema/validate';
import { RunbookPanel } from './views/runbookPanel';

const diagnosticCollection = vscode.languages.createDiagnosticCollection('gert');
const testDiagnosticCollection = vscode.languages.createDiagnosticCollection('gert-test');

export function activate(context: vscode.ExtensionContext) {
  console.log('gert extension activated');

  // Register validate command
  const validateCommand = vscode.commands.registerCommand(
    'gert.validateRunbook',
    () => {
      const editor = vscode.window.activeTextEditor;
      if (!editor) {
        vscode.window.showWarningMessage('Gert: No active editor');
        return;
      }
      if (editor.document.languageId !== 'yaml') {
        vscode.window.showWarningMessage('Gert: Active file is not YAML');
        return;
      }
      validateDocument(editor.document);
    }
  );

  /**
   * Resolve the gert binary path.
   */
  function resolveGertPath(): string {
    const config = vscode.workspace.getConfiguration('gert');
    let gertPath = config.get<string>('binaryPath') || '';
    if (!gertPath || gertPath === 'gert') {
      const candidates = [
        path.join(context.extensionPath, '..', '..', 'bin', 'gert.exe'),
        path.join(context.extensionPath, '..', 'bin', 'gert.exe'),
        'C:\\One\\OpenSource\\gert\\bin\\gert.exe',
      ];
      for (const c of candidates) {
        try { if (fs.existsSync(c)) { gertPath = c; break; } } catch { /* */ }
      }
      if (!gertPath) { gertPath = 'gert'; }
    }
    return gertPath.replace(/\//g, '\\');
  }

  /**
   * Given a TSG markdown path, find or compile the runbook.
   * Returns the runbook path.
   */
  async function resolveRunbook(tsgPath: string, gertPath: string): Promise<string | undefined> {
    const dir = path.dirname(tsgPath);
    const baseName = path.basename(tsgPath, path.extname(tsgPath));
    const runbookPath = path.join(dir, `${baseName}.runbook.yaml`);

    if (fs.existsSync(runbookPath)) {
      // Check if runbook is stale (TSG modified after runbook)
      const tsgStat = fs.statSync(tsgPath);
      const rbStat = fs.statSync(runbookPath);
      if (tsgStat.mtimeMs > rbStat.mtimeMs) {
        const choice = await vscode.window.showQuickPick(
          ['Recompile (TSG changed)', 'Use existing runbook'],
          { placeHolder: `TSG is newer than compiled runbook` }
        );
        if (!choice) { return undefined; }
        if (choice.startsWith('Recompile')) {
          return compileTsg(tsgPath, runbookPath, gertPath);
        }
      }
      return runbookPath;
    }

    // No compiled runbook — ask user
    const action = await vscode.window.showQuickPick(
      ['Compile now', 'Cancel'],
      { placeHolder: `No compiled runbook found for ${path.basename(tsgPath)}. Compile it?` }
    );
    if (action !== 'Compile now') { return undefined; }
    return compileTsg(tsgPath, runbookPath, gertPath);
  }

  /**
   * Compile a TSG markdown into a runbook.
   */
  async function compileTsg(tsgPath: string, outPath: string, gertPath: string): Promise<string | undefined> {
    return vscode.window.withProgress(
      { location: vscode.ProgressLocation.Notification, title: `Compiling TSG: ${path.basename(tsgPath)}`, cancellable: false },
      async () => {
        return new Promise<string | undefined>((resolve) => {
          const { exec } = require('child_process');
          const cmd = `"${gertPath}" compile "${tsgPath}" --out "${outPath}"`;
          const cwd = path.resolve(path.dirname(gertPath), '..');  // project root, where .env lives
          exec(cmd, { cwd, timeout: 120000 }, (err: any, stdout: string, stderr: string) => {
            if (err) {
              const msg = (stderr || err.message || '');
              if (msg.includes('AZURE_OPENAI_ENDPOINT') || msg.includes('AZURE_OPENAI_API_KEY')) {
                vscode.window.showErrorMessage(
                  'Gert: Compilation requires Azure OpenAI credentials. ' +
                  'Create a .env file in the gert project root with AZURE_OPENAI_ENDPOINT, AZURE_OPENAI_API_KEY, and AZURE_OPENAI_DEPLOYMENT.'
                );
              } else {
                // Show first line of error only
                const firstLine = msg.split('\n').find((l: string) => l.trim()) || 'Unknown error';
                vscode.window.showErrorMessage(`Gert: Compilation failed — ${firstLine}`);
              }
              resolve(undefined);
            } else {
              vscode.window.showInformationMessage(`Gert: Compiled ${path.basename(tsgPath)}`);
              resolve(outPath);
            }
          });
        });
      }
    );
  }

  // Register run TSG command — the primary entry point
  const runTsgCommand = vscode.commands.registerCommand(
    'gert.runTsg',
    async (uri?: vscode.Uri) => {
      // Resolve the file path
      let filePath: string;
      if (uri) {
        filePath = uri.fsPath;
      } else {
        const editor = vscode.window.activeTextEditor;
        if (!editor) {
          vscode.window.showWarningMessage('Gert: No active file');
          return;
        }
        filePath = editor.document.uri.fsPath;
      }

      const gertPath = resolveGertPath();
      let runbookPath: string;
      let tsgPath: string | undefined;

      const ext = path.extname(filePath).toLowerCase();
      if (ext === '.md' || ext === '.markdown') {
        // TSG flow: find or compile the runbook
        tsgPath = filePath;
        const resolved = await resolveRunbook(tsgPath, gertPath);
        if (!resolved) { return; }
        runbookPath = resolved;
      } else if (filePath.endsWith('.runbook.yaml') || filePath.endsWith('.runbook.yml')) {
        // Direct runbook (for testing/dev)
        runbookPath = filePath;
      } else {
        vscode.window.showWarningMessage('Gert: Select a TSG (.md) or runbook (.runbook.yaml) file');
        return;
      }

      // Parse the runbook
      let parsed: any;
      try {
        const content = fs.readFileSync(runbookPath, 'utf-8');
        parsed = YAML.parse(content);
      } catch (e: any) {
        vscode.window.showErrorMessage(`Gert: Failed to parse runbook — ${e.message}`);
        return;
      }

      const tsgName = parsed?.meta?.name
        || path.basename(tsgPath || runbookPath).replace(/\.(runbook\.)?(yaml|yml|md)$/gi, '')
        || 'TSG';

      // Pick execution mode
      const modePick = await vscode.window.showQuickPick(
        [
          { label: 'real', description: 'Execute live — prompts for inputs' },
          { label: 'icm', description: 'Execute live — auto-populate inputs from an ICM incident' },
          { label: 'real (saved inputs)', description: 'Execute live — loads inputs from a saved scenario' },
          { label: 'replay', description: 'Replay cached responses from a saved scenario' },
        ],
        { placeHolder: `Run TSG: ${tsgName}` }
      );
      if (!modePick) { return; }
      const loadInputsFromScenario = modePick.label === 'real (saved inputs)' || modePick.label === 'replay';

      // For ICM mode, prompt for the incident ID
      let icmId: string | undefined;
      if (modePick.label === 'icm') {
        icmId = await vscode.window.showInputBox({
          prompt: 'Enter the ICM incident ID',
          title: 'ICM Incident ID',
          placeHolder: 'e.g. 710680757',
          ignoreFocusOut: true,
          validateInput: (val: string) => /^\d+$/.test(val.trim()) ? null : 'Must be a numeric incident ID',
        });
        if (icmId === undefined) { return; }
        icmId = icmId.trim();
      }

      // For replay or saved-inputs, pick scenario
      let scenarioDir: string | undefined;
      if (loadInputsFromScenario) {
        // Convention-based discovery: scenarios/{runbook-name}/*/inputs.yaml
        const runbookName = path.basename(runbookPath).replace(/\.runbook\.(yaml|yml)$/i, '');
        const conventionBase = path.join(path.dirname(runbookPath), 'scenarios', runbookName);
        if (fs.existsSync(conventionBase)) {
          try {
            const dirs = fs.readdirSync(conventionBase, { withFileTypes: true })
              .filter((d: any) => d.isDirectory() && fs.existsSync(path.join(conventionBase, d.name, 'inputs.yaml')))
              .map((d: any) => d.name);
            if (dirs.length === 1) {
              scenarioDir = path.join(conventionBase, dirs[0]);
            } else if (dirs.length > 1) {
              const pick = await vscode.window.showQuickPick(dirs, { placeHolder: `Select scenario (${dirs.length} available)` });
              if (!pick) { return; }
              scenarioDir = path.join(conventionBase, pick);
            }
          } catch { /* ignore */ }
        }

        // Fallback: folder picker
        if (!scenarioDir) {
          const folders = await vscode.window.showOpenDialog({
            canSelectFolders: true, canSelectFiles: false, openLabel: 'Select scenario folder',
          });
          if (!folders || folders.length === 0) { return; }
          scenarioDir = folders[0].fsPath;
        }
        if (scenarioDir && !path.isAbsolute(scenarioDir)) {
          scenarioDir = path.join(path.dirname(runbookPath), scenarioDir);
        }

        // Guard: replay requires step response data
        if (modePick.label === 'replay' && scenarioDir) {
          const stepsDir = path.join(scenarioDir, 'steps');
          const hasSteps = fs.existsSync(stepsDir) && fs.readdirSync(stepsDir).some((f: string) => f.endsWith('.json'));
          if (!hasSteps) {
            const choice = await vscode.window.showWarningMessage(
              `Scenario has no recorded step responses (no steps/ directory). Replay requires captured data. Run live first to record responses.`,
              'Run as real (saved inputs)',
              'Cancel'
            );
            if (choice === 'Run as real (saved inputs)') {
              // Switch to real mode but keep the scenario inputs
              (modePick as any).label = 'real (saved inputs)';
            } else {
              return;
            }
          }
        }
      }

      // Resolve effective mode after possible guard override
      const mode = (modePick.label === 'real (saved inputs)' || modePick.label === 'icm') ? 'real' : modePick.label;
      let vars: Record<string, string> = {};
      if (parsed?.meta?.vars) {
        vars = { ...parsed.meta.vars };
      }

      // In replay or saved-inputs mode, load inputs from scenario
      if (loadInputsFromScenario && scenarioDir) {
        try {
          const inputsPath = path.join(scenarioDir, 'inputs.yaml');
          if (fs.existsSync(inputsPath)) {
            const scenarioInputs = YAML.parse(fs.readFileSync(inputsPath, 'utf-8'));
            if (scenarioInputs && typeof scenarioInputs === 'object') {
              // Coerce all values to strings — YAML may parse numbers/booleans
              for (const [k, v] of Object.entries(scenarioInputs)) {
                vars[k] = String(v);
              }
            }
          }
        } catch { /* ignore */ }
      }

      // In real mode without saved inputs, prompt for unresolved inputs
      if (!loadInputsFromScenario && parsed?.meta?.inputs) {
        for (const [name, input] of Object.entries(parsed.meta.inputs) as [string, any][]) {
          if (vars[name]) { continue; }

          // In ICM mode, skip ALL inputs — the server resolves icm.* bindings
          // and extracts well-known fields from the title. Prompting defeats the purpose.
          if (modePick.label === 'icm') {
            continue;
          }

          // Build a rich prompt with example and source hints
          let promptText = `[${input.from || 'input'}] ${input.description || name}`;
          if (input.example) {
            promptText += ` (e.g. ${input.example})`;
          }

          const value = await vscode.window.showInputBox({
            prompt: promptText,
            title: `Input: ${name}`,
            value: input.default || '',
            placeHolder: input.example ? `e.g. ${input.example}` : `Value for ${name}`,
            ignoreFocusOut: true,
            validateInput: (val: string) => {
              if (!val.trim()) {
                return `${name} cannot be empty`;
              }
              if (input.pattern) {
                try {
                  const re = new RegExp(input.pattern);
                  if (!re.test(val)) {
                    return `Does not match expected pattern: ${input.pattern}${input.example ? ' (e.g. ' + input.example + ')' : ''}`;
                  }
                } catch { /* ignore bad patterns */ }
              }
              return null;
            },
          });
          if (value === undefined) {
            // User cancelled — abort launch
            return;
          }
          vars[name] = value;
        }
      }

      // If we don't have an explicit tsgPath, try meta.source.file
      const sourceFile = tsgPath || parsed?.meta?.source?.file;

      // Load source mapping if available (stepId → source line range)
      let sourceMapping: Record<string, { start: number; end: number }> | undefined;
      const mappingPath = runbookPath.replace(/\.runbook\.(yaml|yml)$/i, '.mapping.md');
      try {
        const fs = await import('fs');
        const mappingContent = fs.readFileSync(mappingPath, 'utf-8');
        sourceMapping = parseMappingTable(mappingContent);
      } catch {
        // no mapping file — sync won't work, that's fine
      }

      try {
        // Extract ICM ID from scenario dir name if not already set
        if (!icmId && scenarioDir) {
          const icmMatch = scenarioDir.match(/icm-(\d+)/);
          if (icmMatch) { icmId = icmMatch[1]; }
        }
        await RunbookPanel.create(context, gertPath, runbookPath, mode, vars, {
          icmId,
          scenarioDir,
          sourceFile,
          tsgName,
          sourceMapping,
          inputDefs: parsed?.meta?.inputs,
        });
      } catch (e: any) {
        vscode.window.showErrorMessage(`Gert: Failed to run — ${e.message}`);
      }
    }
  );

  // Keep old command as alias
  const runCommandAlias = vscode.commands.registerCommand(
    'gert.runRunbook',
    (uri?: vscode.Uri) => vscode.commands.executeCommand('gert.runTsg', uri)
  );

  // Auto-validate on save
  const onSave = vscode.workspace.onDidSaveTextDocument((doc) => {
    if (isRunbookFile(doc)) {
      validateDocument(doc);
      runTestsOnSave(doc, resolveGertPath());
    }
  });

  // Auto-validate on open
  const onOpen = vscode.workspace.onDidOpenTextDocument((doc) => {
    if (isRunbookFile(doc)) { validateDocument(doc); }
  });

  // Feature 2: Batch replay command — Gert: Run Tests
  const runTestsCommand = vscode.commands.registerCommand(
    'gert.runTests',
    async (uri?: vscode.Uri) => {
      let filePath: string;
      if (uri) {
        filePath = uri.fsPath;
      } else {
        const editor = vscode.window.activeTextEditor;
        if (!editor) {
          vscode.window.showWarningMessage('Gert: No active file');
          return;
        }
        filePath = editor.document.uri.fsPath;
      }

      if (!filePath.endsWith('.runbook.yaml') && !filePath.endsWith('.runbook.yml')) {
        vscode.window.showWarningMessage('Gert: Select a .runbook.yaml file to run tests');
        return;
      }

      const gertPath = resolveGertPath();
      await runTestsForRunbook(filePath, gertPath, context);
    }
  );

  context.subscriptions.push(
    validateCommand, runTsgCommand, runCommandAlias, runTestsCommand,
    onSave, onOpen, diagnosticCollection, testDiagnosticCollection
  );
}

/**
 * Parse a .mapping.md file and extract stepId → source line range.
 * Looks for table rows like: | step_id | ... | L32-L49 | ... |
 */
function parseMappingTable(content: string): Record<string, { start: number; end: number }> {
  const result: Record<string, { start: number; end: number }> = {};
  const lines = content.split('\n');
  for (const line of lines) {
    // Match table rows: | stepId | ... | L<start>-L<end> | ... |
    const match = line.match(/^\|\s*(\w+)\s*\|.*?\|\s*L(\d+)-L(\d+)\s*\|/);
    if (match) {
      result[match[1]] = { start: parseInt(match[2], 10), end: parseInt(match[3], 10) };
    }
  }
  return result;
}

function isRunbookFile(doc: vscode.TextDocument): boolean {
  if (doc.languageId !== 'yaml') return false;
  const name = doc.fileName.toLowerCase();
  return name.includes('runbook') || name.endsWith('.yaml') || name.endsWith('.yml');
}

function validateDocument(doc: vscode.TextDocument) {
  const text = doc.getText();
  let parsed: unknown;

  try {
    parsed = YAML.parse(text);
  } catch (e: any) {
    // Show YAML syntax error
    const diagnostic = new vscode.Diagnostic(
      new vscode.Range(0, 0, 0, 0),
      `YAML parse error: ${e.message}`,
      vscode.DiagnosticSeverity.Error
    );
    diagnostic.source = 'gert';
    diagnosticCollection.set(doc.uri, [diagnostic]);
    vscode.window.showErrorMessage(`Gert: YAML parse error — ${e.message}`);
    return;
  }

  if (!parsed || typeof parsed !== 'object') {
    diagnosticCollection.delete(doc.uri);
    vscode.window.showWarningMessage('Gert: File is empty or not a YAML object');
    return;
  }

  try {
    const result = validateRunbook(parsed);
    const diagnostics: vscode.Diagnostic[] = result.errors.map((err: ValidationError) => {
      const range = new vscode.Range(0, 0, 0, 0);
      const diagnostic = new vscode.Diagnostic(
        range,
        `[${err.phase}] ${err.path}: ${err.message}`,
        vscode.DiagnosticSeverity.Error
      );
      diagnostic.source = 'gert';
      return diagnostic;
    });

    diagnosticCollection.set(doc.uri, diagnostics);

    if (result.valid) {
      vscode.window.showInformationMessage(`Gert: ✓ Runbook is valid`);
    } else {
      vscode.window.showErrorMessage(`Gert: ${result.errors.length} validation error(s) — check Problems panel`);
    }
  } catch (e: any) {
    console.error('gert validation error:', e);
    vscode.window.showErrorMessage(`Gert: Schema validation failed — ${e.message}`);
  }
}

/**
 * Feature 2: Run all scenario tests for a runbook via gert test --json.
 * Shows results in a webview panel.
 */
async function runTestsForRunbook(
  runbookPath: string,
  gertPath: string,
  context: vscode.ExtensionContext
): Promise<void> {
  const runbookName = path.basename(runbookPath).replace(/\.runbook\.(yaml|yml)$/i, '');

  await vscode.window.withProgress(
    {
      location: vscode.ProgressLocation.Notification,
      title: `Gert: Running tests for ${runbookName}`,
      cancellable: true,
    },
    async (progress, token) => {
      return new Promise<void>((resolve) => {
        const cwd = path.resolve(path.dirname(gertPath), '..');
        const cmd = `"${gertPath}" test --json "${runbookPath}"`;

        const proc = exec(cmd, { cwd, timeout: 120000 }, (err, stdout, stderr) => {
          if (token.isCancellationRequested) {
            resolve();
            return;
          }

          // Parse JSON output (gert test --json writes to stdout even on failure)
          let output: any;
          try {
            output = JSON.parse(stdout);
          } catch {
            vscode.window.showErrorMessage(
              `Gert: Failed to parse test output — ${stderr || err?.message || 'unknown error'}`
            );
            resolve();
            return;
          }

          // Display in webview
          showTestResultsPanel(output, runbookPath, gertPath, context);

          // Also update diagnostics
          updateTestDiagnostics(runbookPath, output);

          resolve();
        });

        token.onCancellationRequested(() => {
          proc.kill();
        });
      });
    }
  );
}

/**
 * Show test results in a webview panel.
 */
function showTestResultsPanel(
  output: any,
  runbookPath: string,
  gertPath: string,
  context: vscode.ExtensionContext
): void {
  const runbookName = output.runbook || path.basename(runbookPath).replace(/\.runbook\.(yaml|yml)$/i, '');

  const panel = vscode.window.createWebviewPanel(
    'gertTestResults',
    `Tests: ${runbookName}`,
    vscode.ViewColumn.Beside,
    { enableScripts: true }
  );

  const scenarios: any[] = output.scenarios || [];
  const summary = output.summary || { total: 0, passed: 0, failed: 0, skipped: 0 };

  // Build scenario rows
  const rows = scenarios.map((s: any) => {
    const statusIcon = s.status === 'passed' ? '✓'
      : s.status === 'failed' ? '✗'
      : s.status === 'skipped' ? '○'
      : '⚠';
    const statusClass = s.status;
    const outcome = s.outcome
      ? (s.status === 'failed'
        ? `expected: ${s.outcome.expected}, got: ${s.outcome.actual}`
        : s.outcome.actual || '')
      : (s.status === 'skipped' ? 'no test.yaml' : s.error || '');

    const failureDetails = (s.assertions || [])
      .filter((a: any) => !a.passed)
      .map((a: any) => `<div class="assertion-fail">${escapeHtml(a.type)}${a.key ? ' [' + escapeHtml(a.key) + ']' : ''}: ${escapeHtml(a.message)}</div>`)
      .join('');

    const updateBtn = s.status === 'failed' && s.scenario_dir
      ? `<button class="update-btn" onclick="updateTest('${escapeHtml(s.scenario_dir.replace(/\\/g, '\\\\'))}', '${escapeHtml(s.scenario_name)}')">Update test.yaml</button>`
      : '';

    return `
      <tr class="${statusClass}">
        <td class="icon">${statusIcon}</td>
        <td class="name">${escapeHtml(s.scenario_name)}</td>
        <td class="outcome">${escapeHtml(outcome)}</td>
        <td class="duration">${s.duration_ms}ms</td>
      </tr>
      ${failureDetails ? `<tr class="details"><td colspan="4">${failureDetails}${updateBtn}</td></tr>` : ''}
    `;
  }).join('');

  const summaryColor = summary.failed > 0 ? '#f44' : '#4a4';

  panel.webview.html = `<!DOCTYPE html>
<html>
<head>
<style>
  body { font-family: var(--vscode-font-family); color: var(--vscode-foreground); background: var(--vscode-editor-background); padding: 16px; }
  h2 { margin: 0 0 12px 0; font-size: 16px; }
  .summary { font-size: 14px; margin-bottom: 16px; padding: 8px 12px; border-radius: 4px; background: var(--vscode-textBlockQuote-background); }
  .summary .count { font-weight: bold; color: ${summaryColor}; }
  table { width: 100%; border-collapse: collapse; font-size: 13px; }
  th { text-align: left; padding: 6px 8px; border-bottom: 1px solid var(--vscode-panel-border); font-weight: 600; }
  td { padding: 6px 8px; border-bottom: 1px solid var(--vscode-panel-border); }
  tr.passed .icon { color: #4a4; }
  tr.failed .icon { color: #f44; }
  tr.skipped .icon { color: #888; }
  tr.error .icon { color: #fa0; }
  .icon { font-size: 15px; width: 24px; text-align: center; }
  .name { font-weight: 500; }
  .outcome { color: var(--vscode-descriptionForeground); }
  .duration { text-align: right; color: var(--vscode-descriptionForeground); width: 60px; }
  tr.details td { padding: 2px 8px 8px 36px; border-bottom: 1px solid var(--vscode-panel-border); }
  .assertion-fail { color: #f44; font-size: 12px; margin: 2px 0; }
  .actions { margin-top: 16px; }
  button { background: var(--vscode-button-background); color: var(--vscode-button-foreground); border: none; padding: 6px 14px; cursor: pointer; border-radius: 2px; font-size: 12px; margin-right: 8px; }
  button:hover { background: var(--vscode-button-hoverBackground); }
  .update-btn { margin-top: 6px; font-size: 11px; background: var(--vscode-button-secondaryBackground); color: var(--vscode-button-secondaryForeground); }
</style>
</head>
<body>
  <h2>Test Results: ${escapeHtml(runbookName)}</h2>
  <div class="summary">
    <span class="count">${summary.passed} passed</span>,
    <span class="count" style="color: ${summary.failed > 0 ? '#f44' : 'inherit'}">${summary.failed} failed</span>,
    ${summary.skipped} skipped
    — ${summary.total} total
  </div>
  <table>
    <tr><th></th><th>Scenario</th><th>Outcome</th><th style="text-align:right">Time</th></tr>
    ${rows}
  </table>
  <div class="actions">
    <button onclick="rerun()">Re-run All</button>
  </div>
  <script>
    const vscode = acquireVsCodeApi();
    function rerun() { vscode.postMessage({ type: 'rerun' }); }
    function updateTest(scenarioDir, scenarioName) {
      vscode.postMessage({ type: 'updateTest', scenarioDir, scenarioName });
    }
  </script>
</body>
</html>`;

  // Handle messages from webview
  panel.webview.onDidReceiveMessage((msg) => {
    if (msg.type === 'rerun') {
      panel.dispose();
      runTestsForRunbook(runbookPath, gertPath, context);
    } else if (msg.type === 'updateTest') {
      updateTestYamlFromActual(msg.scenarioDir, runbookPath, gertPath, () => {
        // Re-run after update
        panel.dispose();
        runTestsForRunbook(runbookPath, gertPath, context);
      });
    }
  });
}

function escapeHtml(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
}

/**
 * Feature 3: Update diagnostics from test results.
 * Called after gert test --json completes (both on-save and explicit run).
 */
function updateTestDiagnostics(runbookPath: string, output: any) {
  const diagnostics: vscode.Diagnostic[] = [];
  const scenarios: any[] = output.scenarios || [];

  for (const s of scenarios) {
    if (s.status !== 'failed') continue;
    const assertions: any[] = s.assertions || [];
    for (const a of assertions) {
      if (a.passed) continue;
      const diagnostic = new vscode.Diagnostic(
        new vscode.Range(0, 0, 0, 0),
        `Scenario ${s.scenario_name}: ${a.message}`,
        vscode.DiagnosticSeverity.Warning
      );
      diagnostic.source = 'gert test';
      diagnostics.push(diagnostic);
    }
  }

  const uri = vscode.Uri.file(runbookPath);
  testDiagnosticCollection.set(uri, diagnostics);
}

/**
 * Feature 3: Run tests in the background after saving a .runbook.yaml.
 * Debounced to avoid repeated spawns during rapid saves.
 */
/**
 * Feature 5: Update test.yaml from actual results.
 * Re-runs the specific scenario, captures the actual outcome/captures/steps,
 * and writes them as the new expected values in test.yaml.
 */
function updateTestYamlFromActual(
  scenarioDir: string,
  runbookPath: string,
  gertPath: string,
  onComplete: () => void
) {
  const cwd = path.resolve(path.dirname(gertPath), '..');
  const cmd = `"${gertPath}" test --json "${runbookPath}"`;

  exec(cmd, { cwd, timeout: 60000 }, (err, stdout, _stderr) => {
    try {
      const output = JSON.parse(stdout);
      const scenarios: any[] = output.scenarios || [];
      // Find the matching scenario by directory
      const normalizedDir = scenarioDir.replace(/\\/g, '/').toLowerCase();
      const match = scenarios.find((s: any) =>
        (s.scenario_dir || '').replace(/\\/g, '/').toLowerCase() === normalizedDir
      );

      if (!match) {
        vscode.window.showErrorMessage('Gert: Could not find scenario in test results');
        return;
      }

      // Build new test.yaml from actual results
      const newSpec: Record<string, any> = {};
      if (match.outcome) {
        newSpec.expected_outcome = match.outcome.actual;
      }
      // Preserve description and tags from existing test.yaml if present
      const existingTestPath = path.join(scenarioDir, 'test.yaml');
      if (fs.existsSync(existingTestPath)) {
        try {
          const existing = YAML.parse(fs.readFileSync(existingTestPath, 'utf-8'));
          if (existing?.description) { newSpec.description = existing.description; }
          if (existing?.tags) { newSpec.tags = existing.tags; }
          // Carry forward must_reach/must_not_reach from existing if present
          if (existing?.must_not_reach) { newSpec.must_not_reach = existing.must_not_reach; }
        } catch { /* ignore parse errors */ }
      }

      const testYaml = YAML.stringify(newSpec);
      fs.writeFileSync(existingTestPath, testYaml, 'utf-8');
      vscode.window.showInformationMessage(`Gert: Updated test.yaml for ${match.scenario_name}`);
    } catch (e: any) {
      vscode.window.showErrorMessage(`Gert: Failed to update test.yaml — ${e.message}`);
    }
    onComplete();
  });
}

let testOnSaveTimer: ReturnType<typeof setTimeout> | undefined;
let testOnSaveProc: ReturnType<typeof exec> | undefined;

function runTestsOnSave(doc: vscode.TextDocument, gertPath: string) {
  // Only run for .runbook.yaml files
  const fileName = doc.fileName.toLowerCase();
  if (!fileName.endsWith('.runbook.yaml') && !fileName.endsWith('.runbook.yml')) {
    return;
  }

  // Check if there are any scenarios with test.yaml
  const runbookName = path.basename(doc.fileName).replace(/\.runbook\.(yaml|yml)$/i, '');
  const scenariosBase = path.join(path.dirname(doc.fileName), 'scenarios', runbookName);
  if (!fs.existsSync(scenariosBase)) {
    testDiagnosticCollection.delete(doc.uri);
    return;
  }

  // Check if any scenario has test.yaml
  let hasTests = false;
  try {
    const dirs = fs.readdirSync(scenariosBase, { withFileTypes: true });
    for (const d of dirs) {
      if (d.isDirectory() && fs.existsSync(path.join(scenariosBase, d.name, 'test.yaml'))) {
        hasTests = true;
        break;
      }
    }
  } catch { /* ignore */ }

  if (!hasTests) {
    testDiagnosticCollection.delete(doc.uri);
    return;
  }

  // Debounce: cancel previous timer and process
  if (testOnSaveTimer) {
    clearTimeout(testOnSaveTimer);
  }
  if (testOnSaveProc) {
    testOnSaveProc.kill();
    testOnSaveProc = undefined;
  }

  testOnSaveTimer = setTimeout(() => {
    const cwd = path.resolve(path.dirname(gertPath), '..');
    const cmd = `"${gertPath}" test --json "${doc.fileName}"`;

    testOnSaveProc = exec(cmd, { cwd, timeout: 30000 }, (err, stdout, _stderr) => {
      testOnSaveProc = undefined;
      try {
        const output = JSON.parse(stdout);
        updateTestDiagnostics(doc.fileName, output);
      } catch {
        // Silently ignore parse failures in background testing
      }
    });
  }, 500);
}

export function deactivate() {
  diagnosticCollection.dispose();
  testDiagnosticCollection.dispose();
  if (testOnSaveTimer) { clearTimeout(testOnSaveTimer); }
  if (testOnSaveProc) { testOnSaveProc.kill(); }
}
