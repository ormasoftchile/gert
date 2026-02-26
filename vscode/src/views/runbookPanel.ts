import * as vscode from 'vscode';
import hljs from 'highlight.js/lib/core';
import hljsSql from 'highlight.js/lib/languages/sql';
import { marked } from 'marked';
import { GertClient, StepSummary, RPCMessage, StartResult, DisplayConfig } from '../serve/client';

// Register SQL language (covers T-SQL, KQL basics)
hljs.registerLanguage('sql', hljsSql);

// Register a lightweight Kusto (KQL) grammar for highlight.js
hljs.registerLanguage('kusto', function() {
  return {
    case_insensitive: true,
    keywords: {
      keyword: 'where summarize project extend order take top count join union render distinct parse evaluate let by as asc desc and or not in has has_any contains startswith endswith between',
      built_in: 'ago now bin datetime timespan todynamic parse_json isempty isnotempty isnull isnotnull coalesce iff iif case strlen substring indexof replace trim toupper tolower hash countif sumif dcount count_distinct avg sum min max make_set make_list strcat strcat_array split tostring toint tolong todouble todatetime format_datetime round',
      literal: 'true false null'
    },
    contains: [
      hljs.QUOTE_STRING_MODE,
      hljs.APOS_STRING_MODE,
      hljs.C_LINE_COMMENT_MODE,
      hljs.C_NUMBER_MODE,
      {
        className: 'operator',
        begin: /\|/
      },
      {
        className: 'variable',
        begin: /\{\{/, end: /\}\}/
      },
      {
        className: 'type',
        begin: /\b(Mon\w+|Sql\w+|MonLogin|MonRedirector|MonRgLoad|MonSqlSystemHealth)\b/
      }
    ]
  };
});

/** A snapshot of one TSG execution in a chain. */
interface ChainEntry {
  name: string;
  runbookPath: string;
  inputs: Record<string, string>;
  captures: Record<string, string>;
  steps: { id: string; title: string; state: string }[];
  tree: any[];
  stepStates: Map<string, string>;
  stepDetails: Map<string, any>;
  prose: any;
  description: string;
  outcome: { state: string; recommendation: string; isRouting?: boolean } | null;
}

/**
 * RunbookPanel manages the three-panel layout for runbook execution.
 * - Left: source TSG prose (standard editor)
 * - Middle: workflow map (webview)
 * - Right: active step detail (webview)
 */
export class RunbookPanel {
  private panel: vscode.WebviewPanel;
  private client: GertClient;
  private steps: StepSummary[] = [];
  private currentStepIndex = -1;
  private stepStates: Map<string, string> = new Map();
  private stepDetails: Map<string, any> = new Map(); // cached step details from execution events
  private captures: Record<string, string> = {};
  private disposed = false;
  private currentStepDetail: any = null;
  private outcomeResult: any = null;
  private pendingNextRunbook: { file: string; inputs?: Record<string, string> } | null = null;
  private extensionContext: vscode.ExtensionContext | null = null;
  private chainHistory: ChainEntry[] = [];
  private stepErrors: Map<string, string> = new Map();
  private tree: any[] = [];  // tree structure from gert serve
  private processing = false; // true while waiting for server after user action
  private nextInFlight = false; // true while a case 'next' handler is executing (prevents stale auto-advance)
  private runCompleted = false; // true when all steps are done (no outcome)
  private runbookKind = 'mitigation'; // kind from meta.kind
  private viewingStepId: string | null = null; // step being browsed (non-active)
  private stepHistory: string[] = []; // history of viewed step IDs for back nav
  private creationArgs: { runbookPath: string; mode: string; vars: Record<string, string>; options?: any } | null = null;
  private originalCreationArgs: { runbookPath: string; mode: string; vars: Record<string, string>; options?: any } | null = null;
  private inputDefs: Record<string, { from?: string; description?: string; example?: string }> = {}; // input definitions from meta
  private sourceFilePath: string | undefined; // path to source TSG markdown
  private sourceDirUri: vscode.Uri | undefined; // directory of source TSG (for image resolution)
  private sourceMapping: Record<string, { start: number; end: number }> = {}; // stepId → source line range
  private runBaseDir: string = ''; // engine run artifacts dir (relative to gertCwd)
  private chainBaseDirs: string[] = []; // all baseDirs from parent + child runs (for chained scenario save)
  private gertCwd: string = ''; // gert serve process CWD
  private autoRunning = false; // true during "Run All" replay
  private iterateState: { pass: number; max: number; status: 'running' | 'converged' | 'failed'; error?: string; mode?: 'over' | 'until'; total?: number; item?: string; as?: string } | null = null;
  private leftTab: 'map' | 'prose' = 'map'; // which tab is shown in the left column
  private proseWidth: string = '40%'; // persisted prose panel width
  private mapWidth: string = '260px'; // persisted workflow map width
  private prose: any = null; // prose sections from runbook meta
  private runbookDescription: string = ''; // meta.description
  private displayConfig: DisplayConfig = {}; // display preferences from settings → server → UI gating
  private highlightDecoration = vscode.window.createTextEditorDecorationType({
    backgroundColor: new vscode.ThemeColor('editor.findMatchHighlightBackground'),
    isWholeLine: true,
    overviewRulerColor: new vscode.ThemeColor('editorOverviewRuler.findMatchForeground'),
  });

  private constructor(panel: vscode.WebviewPanel, client: GertClient) {
    this.panel = panel;
    this.client = client;

    // Listen for events from gert serve
    client.onEvent((msg: RPCMessage) => {
      this.handleEvent(msg);
    });

    // Handle messages from the webview
    panel.webview.onDidReceiveMessage((msg) => {
      this.handleWebviewMessage(msg);
    });

    panel.onDidDispose(() => {
      this.disposed = true;
      client.shutdown();
    });
  }

  /** Read display preferences from VS Code settings. */
  private static readDisplaySettings(): DisplayConfig {
    const cfg = vscode.workspace.getConfiguration('gert');
    return {
      debugTrace: cfg.get<boolean>('debugTrace'),
      captures: cfg.get<boolean>('showCaptures'),
      outcomeConditions: cfg.get<boolean>('showOutcomeConditions'),
      copySummary: cfg.get<boolean>('showCopySummary'),
      saveForReplay: cfg.get<boolean>('showSaveForReplay'),
    };
  }

  /** Resolve effective visibility: debugTrace=true overrides captures & outcomeConditions to true. */
  private showCaptures(): boolean {
    if (this.displayConfig.debugTrace) return true;
    return this.displayConfig.captures ?? false;
  }
  private showOutcomeConditions(): boolean {
    if (this.displayConfig.debugTrace) return true;
    return this.displayConfig.outcomeConditions ?? false;
  }
  private showCopySummary(): boolean {
    return this.displayConfig.copySummary ?? true;
  }
  private showSaveForReplay(): boolean {
    return this.displayConfig.saveForReplay ?? true;
  }

  /**
   * Create and show the runbook panel, start execution.
   */
  static async create(
    context: vscode.ExtensionContext,
    gertPath: string,
    runbookPath: string,
    mode: string,
    vars: Record<string, string>,
    options?: { scenarioDir?: string; sourceFile?: string; tsgName?: string; sourceMapping?: Record<string, { start: number; end: number }>; inputDefs?: Record<string, any>; chainHistory?: ChainEntry[] }
  ): Promise<RunbookPanel> {
    // Create the client
    const client = new GertClient(gertPath);
    await client.start();

    // Derive a display name: tsgName > filename without .runbook.yaml
    const fileName = runbookPath.split(/[\\/]/).pop() || '';
    const displayName = options?.tsgName || fileName.replace(/\.runbook\.(yaml|yml)$/i, '') || fileName;

    // Create the webview panel (middle + right combined)
    // Compute localResourceRoots for image loading
    const localRoots: vscode.Uri[] = [];
    if (options?.sourceFile) {
      const sourceDir = vscode.Uri.file(require('path').dirname(options.sourceFile));
      localRoots.push(sourceDir);
      // Also allow parent dir to cover relative _media/ paths
      localRoots.push(vscode.Uri.file(require('path').dirname(require('path').dirname(options.sourceFile))));
    }

    // Build tab title: truncated TSG name
    const shortName = displayName.length > 25 ? displayName.substring(0, 25) + '…' : displayName;
    const tabTitle = shortName;

    const panel = vscode.window.createWebviewPanel(
      'gertRunbook',
      tabTitle,
      vscode.ViewColumn.Active,
      {
        enableScripts: true,
        retainContextWhenHidden: true,
        localResourceRoots: localRoots.length > 0 ? localRoots : undefined,
      }
    );

    const rbPanel = new RunbookPanel(panel, client);

    // Open source TSG in the left panel as a rendered webview
    if (options?.sourceFile) {
      rbPanel.sourceFilePath = options.sourceFile;
      rbPanel.sourceDirUri = vscode.Uri.file(require('path').dirname(options.sourceFile));
      if (options.sourceMapping) {
        rbPanel.sourceMapping = options.sourceMapping;
      }
    }

    // Start execution
    try {
      // Determine the workspace folder containing the runbook for CWD
      const runbookUri = vscode.Uri.file(runbookPath);
      const wsFolder = vscode.workspace.getWorkspaceFolder(runbookUri);
      const cwd = wsFolder ? wsFolder.uri.fsPath : require('path').dirname(runbookPath);

      const displaySettings = RunbookPanel.readDisplaySettings();
      const result = await client.execStart({
        runbook: runbookPath,
        mode,
        vars,
        cwd,
        scenarioDir: options?.scenarioDir,
        display: displaySettings,
      });

      rbPanel.displayConfig = (result as any).display || displaySettings;
      rbPanel.steps = result.steps || [];
      rbPanel.tree = (result as any).tree || [];
      rbPanel.runbookKind = (result as any).kind || 'mitigation';
      rbPanel.prose = (result as any).prose || null;
      rbPanel.runbookDescription = (result as any).description || '';
      rbPanel.runBaseDir = result.baseDir || '';
      rbPanel.chainBaseDirs = result.baseDir ? [result.baseDir] : [];
      rbPanel.gertCwd = cwd; // serve CWD = workspace folder containing the runbook
      // Save creation args for restart
      rbPanel.creationArgs = { runbookPath, mode, vars, options };
      rbPanel.originalCreationArgs = { runbookPath, mode, vars, options };
      rbPanel.extensionContext = context;
      // Restore splitter positions from globalState
      rbPanel.proseWidth = context.globalState.get<string>('gert.proseWidth', '40%');
      rbPanel.mapWidth = context.globalState.get<string>('gert.mapWidth', '260px');
      // Inherit chain history from parent TSG
      if (options?.chainHistory) {
        rbPanel.chainHistory = options.chainHistory;
      }
      // Store input definitions for display
      if (options?.inputDefs) {
        rbPanel.inputDefs = options.inputDefs;
      }
      // Initialize all steps as pending
      for (const step of rbPanel.steps) {
        rbPanel.stepStates.set(step.id, 'pending');
      }
      // Also initialize tree step states
      initTreeStates(rbPanel.tree, rbPanel.stepStates);
      rbPanel.updateWebview();

      // Auto-start: execute the first step immediately
      vscode.window.showInformationMessage(`Gert: Run ${result.runId} started (${result.stepCount} steps, mode: ${mode})`);
      try {
        await client.execNext();
      } catch (e: any) {
        vscode.window.showErrorMessage(`Gert: First step failed — ${e.message}`);
      }
    } catch (e: any) {
      vscode.window.showErrorMessage(`Gert: Failed to start — ${e.message}`);
    }

    return rbPanel;
  }

  /**
   * Handle events from gert serve.
   */
  private handleEvent(msg: RPCMessage) {
    switch (msg.method) {
      case 'event/stepStarted':
        this.processing = false;
        this.viewingStepId = null; // return to active step on advance
        this.stepStates.set(msg.params.stepId, 'running');
        this.currentStepIndex = msg.params.index;
        this.currentStepDetail = msg.params;
        this.stepDetails.set(msg.params.stepId, msg.params); // cache for later browsing
        this.outcomeResult = null;
        this.updateWebview();

        // Auto-advance manual steps with outcomes during Run All replay
        if (this.autoRunning && msg.params.type === 'manual' && !msg.params.invokeChild) {
          this.autoAdvanceStep();
        }
        break;

      case 'event/stepCompleted':
        this.processing = false;
        this.stepStates.set(msg.params.stepId, msg.params.status);
        if (msg.params.captures) {
          Object.assign(this.captures, msg.params.captures);
        }
        if (msg.params.error) {
          this.stepErrors.set(msg.params.stepId, msg.params.error);
        }
        this.updateWebview();

        // Invoke child steps are driven by the serve layer — never auto-advance them.
        if (msg.params.invokeChild) break;

        // If a case 'next' handler is already in-flight (awaiting exec/next response),
        // do NOT schedule another auto-advance — the in-flight handler will drive
        // the next step when the RPC response arrives. Without this guard, the
        // delayed timer fires after the manual step is already displayed and sends
        // a spurious exec/next that marks-complete without user choice.
        if (this.nextInFlight) break;

        // Auto-advance: non-manual passed steps need no user interaction.
        // The query ran, data was captured — advance to the next step automatically.
        if (this.autoRunning) {
          setTimeout(() => this.autoAdvanceStep(), 50);
        } else if (msg.params.status === 'passed' && msg.params.type !== 'manual') {
          const autoMode = vscode.workspace.getConfiguration('gert').get<string>('autoAdvanceMode', 'animated');
          if (autoMode === 'manual') break; // User wants full manual control
          // Interactive mode: auto-advance CLI/tool steps that passed
          // Brief flash so the user sees captures before moving on
          const delay = autoMode === 'summary' ? 100 : 600;
          setTimeout(async () => {
            // Double-check: if a next call started between schedule and fire, bail out
            if (this.nextInFlight) return;
            try {
              this.processing = true;
              this.updateWebview();
              const nextResult = await this.client.execNext();
              if (nextResult?.status === 'awaiting_user') {
                this.processing = false;
              }
              if (nextResult?.status === 'completed' && !this.outcomeResult) {
                this.processing = false;
                this.runCompleted = true;
                this.currentStepDetail = null;
              }
              this.updateWebview();
            } catch (e: any) {
              this.processing = false;
              this.updateWebview();
            }
          }, delay);
        }
        break;

      case 'event/stepSkipped':
        this.stepStates.set(msg.params.stepId, 'skipped');
        this.updateWebview();
        break;

      case 'event/inputRequired':
        this.panel.webview.postMessage({
          type: 'inputRequired',
          ...msg.params,
        });
        break;

      case 'event/outcomeReached':
        this.processing = false;
        this.autoRunning = false;
        this.outcomeResult = msg.params;
        if (msg.params.nextRunbook) {
          this.pendingNextRunbook = msg.params.nextRunbook;
          // Auto-chain to next TSG immediately — no stop, no button
          this.updateWebview();
          this.handleWebviewMessage({ type: 'chainToRunbook' });
        } else {
          this.updateWebview();
        }
        break;

      case 'event/runCompleted':
        this.processing = false;
        this.autoRunning = false;
        if (!this.outcomeResult) {
          this.runCompleted = true;
          this.currentStepDetail = null;
        }
        this.updateWebview();
        break;

      case 'event/iterateStarted':
        if (msg.params.mode === 'over') {
          this.iterateState = { pass: msg.params.pass || 1, max: msg.params.total || 0, status: 'running', mode: 'over', total: msg.params.total, item: msg.params.item, as: msg.params.as };
        } else {
          this.iterateState = { pass: 1, max: msg.params.max, status: 'running', mode: 'until' };
        }
        this.updateWebview();
        break;

      case 'event/iteratePass':
        if (msg.params.mode === 'over') {
          this.iterateState = { pass: msg.params.pass, max: msg.params.total || this.iterateState?.max || 0, status: 'running', mode: 'over', total: msg.params.total, item: msg.params.item, as: this.iterateState?.as };
        } else {
          this.iterateState = { pass: msg.params.pass, max: msg.params.max, status: 'running', mode: 'until' };
        }
        this.updateWebview();
        break;

      case 'event/iterateConverged':
        if (msg.params.mode === 'over') {
          this.iterateState = { pass: msg.params.pass || msg.params.total, max: msg.params.total || this.iterateState?.max || 0, status: 'converged', mode: 'over', total: msg.params.total };
        } else {
          this.iterateState = { pass: msg.params.pass, max: msg.params.max, status: 'converged', mode: 'until' };
        }
        this.updateWebview();
        break;

      case 'event/iterateFailed':
        this.processing = false;
        this.iterateState = { pass: msg.params.max, max: msg.params.max, status: 'failed', error: msg.params.error, mode: msg.params.mode || 'until' };
        vscode.window.showErrorMessage(`Iterate did not converge: ${msg.params.error}`);
        this.updateWebview();
        break;

      case 'runbook/staleSource':
        vscode.window.showWarningMessage(
          `Source TSG has changed since this runbook was compiled (${msg.params.compiledAt}). Consider recompiling.`,
          'Recompile'
        ).then(choice => {
          if (choice === 'Recompile') {
            vscode.commands.executeCommand('gert.compile', msg.params.sourceFile);
          }
        });
        break;
    }
  }

  /**
   * Handle messages from the webview (user actions).
   */
  private async handleWebviewMessage(msg: any) {
    switch (msg.type) {
      case 'next':
        try {
          this.processing = true;
          this.nextInFlight = true;
          this.updateWebview();
          let nextResult = await this.client.execNext();

          const autoAdvanceMode = vscode.workspace.getConfiguration('gert').get<string>('autoAdvanceMode', 'animated');

          if (autoAdvanceMode === 'animated') {
            // Animated: auto-advance with a pause so Steps flash in the map
            while (nextResult && nextResult.status !== 'awaiting_user' && nextResult.status !== 'completed' && nextResult.status !== 'outcome' && !this.outcomeResult) {
              // Brief pause so the step is visible in the map
              await new Promise(r => setTimeout(r, 300));
              this.updateWebview();
              nextResult = await this.client.execNext();
            }
          } else if (autoAdvanceMode === 'summary') {
            // Summary: auto-advance instantly, no pauses
            while (nextResult && nextResult.status !== 'awaiting_user' && nextResult.status !== 'completed' && nextResult.status !== 'outcome' && !this.outcomeResult) {
              nextResult = await this.client.execNext();
            }
          }
          // 'manual' mode: no auto-advance loop — each step waits for user click

          this.nextInFlight = false;

          // Merge result data (e.g. choices) into currentStepDetail so the webview can render them
          if (nextResult && this.currentStepDetail && nextResult.stepId === this.currentStepDetail.stepId) {
            if (nextResult.choices) { this.currentStepDetail.choices = nextResult.choices; }
            if (nextResult.hasOutcomes !== undefined) { this.currentStepDetail.hasOutcomes = nextResult.hasOutcomes; }
            this.stepDetails.set(this.currentStepDetail.stepId, this.currentStepDetail);
          }

          // Clear processing when awaiting user input so buttons render
          if (nextResult?.status === 'awaiting_user') {
            this.processing = false;
          }
          this.updateWebview();

          // If tree execution is fully completed with no more steps
          if (nextResult?.status === 'completed' && !this.outcomeResult) {
            this.processing = false;
            this.runCompleted = true;
            this.currentStepDetail = null;
            this.updateWebview();
          }
        } catch (e: any) {
          this.processing = false;
          this.nextInFlight = false;
          this.updateWebview();
          vscode.window.showErrorMessage(`Gert: Step failed — ${e.message}`);
        }
        break;

      case 'chooseOutcome':
        try {
          this.processing = true;
          this.updateWebview();
          const outcomeResult = await this.client.chooseOutcome(msg.stepId, msg.state);
          if (outcomeResult?.status === 'completed' && !this.outcomeResult) {
            this.processing = false;
            this.runCompleted = true;
            this.currentStepDetail = null;
            this.updateWebview();
          }
        } catch (e: any) {
          this.processing = false;
          this.updateWebview();
          vscode.window.showErrorMessage(`Gert: Outcome failed — ${e.message}`);
        }
        break;

      case 'chainToRunbook':
        try {
          if (!this.pendingNextRunbook) { break; }
          const nr = this.pendingNextRunbook;
          const pathMod = require('path');
          const fsMod = require('fs');
          const parentDir = pathMod.dirname(this.creationArgs?.runbookPath || '');
          let childMdPath = pathMod.resolve(parentDir, nr.file);
          let childRunbookPath = childMdPath.replace(/\.md$/i, '.runbook.yaml');
          const gertPath2 = this.client.getGertPath();

          // Auto-compile if needed
          if (!fsMod.existsSync(childRunbookPath)) {
            if (!fsMod.existsSync(childMdPath)) {
              // Specific file doesn't exist — try fallback to directory index
              // e.g. login-errors/error-26078-state-33.md → login-errors/login-errors.md
              const dirName = pathMod.dirname(childMdPath);
              const dirBaseName = pathMod.basename(dirName);
              const fallbackMd = pathMod.join(dirName, `${dirBaseName}.md`);
              const fallbackRb = fallbackMd.replace(/\.md$/i, '.runbook.yaml');
              if (fsMod.existsSync(fallbackMd) || fsMod.existsSync(fallbackRb)) {
                vscode.window.showWarningMessage(`${pathMod.basename(nr.file)} does not exist. Falling back to ${dirBaseName}.md`);
                childMdPath = fallbackMd;
                childRunbookPath = fallbackRb;
              } else {
                vscode.window.showErrorMessage(`Child TSG not found: ${pathMod.basename(nr.file)}`);
                break;
              }
            }
            const compileOk: boolean = await vscode.window.withProgress(
              { location: vscode.ProgressLocation.Notification, title: `Compiling: ${pathMod.basename(nr.file)}`, cancellable: false },
              () => new Promise<boolean>((resolve) => {
                const { exec: execCmd } = require('child_process');
                const cmd = `"${gertPath2}" compile "${childMdPath}" --out "${childRunbookPath}"`;
                const cwd = pathMod.resolve(pathMod.dirname(gertPath2), '..');
                execCmd(cmd, { cwd, timeout: 120000 }, (err: any, _stdout: string, stderr: string) => {
                  if (err) {
                    vscode.window.showErrorMessage(`Compile failed: ${(stderr || err.message || '').split('\n')[0]}`);
                    resolve(false);
                  } else {
                    resolve(true);
                  }
                });
              })
            );
            if (!compileOk || !fsMod.existsSync(childRunbookPath)) { break; }
          }

          // ── Stay in same panel: snapshot, swap client, merge tree ──
          // 1. Snapshot current state into chain history
          this.chainHistory.push(this.snapshotChainEntry());

          // 2. Kill old client, start new one for child runbook
          this.client.shutdown();
          const newClient = new GertClient(gertPath2);
          await newClient.start();
          this.client = newClient;
          newClient.onEvent((evt: RPCMessage) => this.handleEvent(evt));

          // 3. Build child vars and start execution
          const childVars: Record<string, string> = { ...(this.creationArgs?.vars || {}), ...(nr.inputs || {}) };
          const childName = pathMod.basename(nr.file).replace(/\.(runbook\.)?(yaml|yml|md)$/gi, '');
          const childSourceFile = fsMod.existsSync(childMdPath) ? childMdPath : undefined;

          // Preserve mode and scenarioDir from parent — critical for chained replay
          const parentMode = this.creationArgs?.mode || 'real';
          const parentScenarioDir = this.creationArgs?.options?.scenarioDir;

          // Derive CWD for child runbook (may be in different workspace folder)
          const childUri = vscode.Uri.file(childRunbookPath);
          const childWsFolder = vscode.workspace.getWorkspaceFolder(childUri);
          const childCwd = childWsFolder ? childWsFolder.uri.fsPath : require('path').dirname(childRunbookPath);

          const result = await newClient.execStart({
            runbook: childRunbookPath,
            mode: parentMode,
            vars: childVars,
            cwd: childCwd,
            scenarioDir: parentScenarioDir,
            display: RunbookPanel.readDisplaySettings(),
          });
          this.displayConfig = (result as any).display || this.displayConfig;
          this.gertCwd = childCwd;

          // 4. Update panel state for the child TSG (WITHOUT clearing chain history)
          this.creationArgs = { runbookPath: childRunbookPath, mode: parentMode, vars: childVars, options: { sourceFile: childSourceFile, tsgName: childName, scenarioDir: parentScenarioDir } };
          this.steps = result.steps || [];
          this.tree = (result as any).tree || [];
          this.runbookKind = (result as any).kind || 'mitigation';
          this.prose = (result as any).prose || null;
          this.runbookDescription = (result as any).description || '';
          this.runBaseDir = result.baseDir || '';
          if (result.baseDir) { this.chainBaseDirs.push(result.baseDir); }
          this.currentStepIndex = -1;
          this.currentStepDetail = null;
          this.outcomeResult = null;
          this.iterateState = null;
          this.pendingNextRunbook = null;
          this.runCompleted = false;
          this.viewingStepId = null;
          this.stepHistory = [];
          this.captures = {};
          this.stepErrors.clear();
          this.stepDetails.clear(); // clear parent step details for clean child rendering
          // Keep stepStates from parent — add child steps as pending
          const initChildStates = (nodes: any[]) => {
            for (const n of nodes) {
              if (n.step?.id) {
                this.stepStates.set(n.step.id, 'pending');
              }
              if (n.iterate && n.iterate.steps) initChildStates(n.iterate.steps);
              if (n.branches) {
                for (const b of n.branches) {
                  if (b.steps) initChildStates(b.steps);
                }
              }
            }
          };
          if (this.tree.length > 0) initChildStates(this.tree);

          // Update panel title for child runbook
          const currentTitle = this.panel.title;
          const childShort = childName.length > 25 ? childName.substring(0, 25) + '…' : childName;
          this.panel.title = childShort;

          // 5. Update source path for image resolution and recreate prose panel
          if (childSourceFile) {
            this.sourceFilePath = childSourceFile;
            this.sourceDirUri = vscode.Uri.file(pathMod.dirname(childSourceFile));
          }

          this.processing = false;
          this.updateWebview();

          // 6. Auto-advance the first step of the child TSG
          this.handleWebviewMessage({ type: 'next' });
        } catch (e: any) {
          this.processing = false;
          this.updateWebview();
          vscode.window.showErrorMessage(`Gert: Chain failed: ${e.message}`);
        }
        break;

      case 'submitChoice':
        try {
          await this.client.submitChoice(msg.stepId, msg.variable, msg.value);
          // Store the choice locally as a capture
          this.captures[msg.variable] = msg.value;
          // Auto-advance: the choice was made, now mark complete
          this.handleWebviewMessage({ type: 'next' });
        } catch (e: any) {
          vscode.window.showErrorMessage(`Gert: Choice failed — ${e.message}`);
        }
        break;

      case 'submitEvidence':
        try {
          await this.client.submitEvidence(msg.stepId, msg.evidence);
        } catch (e: any) {
          vscode.window.showErrorMessage(`Gert: Submit failed — ${e.message}`);
        }
        break;

      case 'switchTab':
        this.leftTab = msg.tab === 'prose' ? 'prose' : 'map';
        this.updateWebview();
        break;

      case 'saveSplitter':
        if (msg.panel === 'prose') this.proseWidth = msg.width;
        if (msg.panel === 'map') this.mapWidth = msg.width;
        // Persist to globalState
        if (this.extensionContext) {
          this.extensionContext.globalState.update('gert.proseWidth', this.proseWidth);
          this.extensionContext.globalState.update('gert.mapWidth', this.mapWidth);
        }
        break;

      case 'getVariables':
        try {
          const vars = await this.client.getVariables();
          this.panel.webview.postMessage({ type: 'variables', ...vars });
        } catch (e: any) {
          console.error('getVariables failed:', e);
        }
        break;

      case 'viewStep': {
        // Set viewing step and update — works for both current and chain history steps
        if (this.viewingStepId) {
          this.stepHistory.push(this.viewingStepId);
        } else if (this.currentStepDetail) {
          this.stepHistory.push(this.currentStepDetail.stepId);
        }
        this.viewingStepId = msg.stepId;
        this.updateWebview();
        break;
      }

      case 'backStep': {
        if (this.stepHistory.length > 0) {
          const prevId = this.stepHistory.pop()!;
          this.viewingStepId = prevId;
          // If going back to the active step, clear viewing mode
          if (this.currentStepDetail && prevId === this.currentStepDetail.stepId) {
            this.viewingStepId = null;
          }
          this.highlightSourceStep(prevId);
          this.updateWebview();
        }
        break;
      }

      case 'returnToActive': {
        this.viewingStepId = null;
        this.stepHistory = [];
        if (this.currentStepDetail) {
          this.highlightSourceStep(this.currentStepDetail.stepId);
        }
        this.updateWebview();
        break;
      }

      case 'runAll': {
        if (this.creationArgs?.mode === 'replay' && !this.autoRunning) {
          this.autoRunning = true;
          this.updateWebview();
          this.autoAdvanceStep();
        }
        break;
      }

      case 'stopRunAll': {
        this.autoRunning = false;
        this.updateWebview();
        break;
      }

      case 'restart': {
        try {
          this.processing = true;
          this.updateWebview();
          // Shutdown old client and start fresh
          const gertPath = this.client.getGertPath();
          this.client.shutdown();
          const newClient = new GertClient(gertPath);
          await newClient.start();
          this.client = newClient;
          newClient.onEvent((evt: RPCMessage) => this.handleEvent(evt));

          const args = this.originalCreationArgs || this.creationArgs!;
          // Reuse stored CWD or derive from runbook path
          const restartUri = vscode.Uri.file(args.runbookPath);
          const restartWsFolder = vscode.workspace.getWorkspaceFolder(restartUri);
          const restartCwd = restartWsFolder ? restartWsFolder.uri.fsPath : require('path').dirname(args.runbookPath);

          const result = await newClient.execStart({
            runbook: args.runbookPath,
            mode: args.mode,
            vars: args.vars,
            cwd: restartCwd,
            scenarioDir: args.options?.scenarioDir,
            display: RunbookPanel.readDisplaySettings(),
          });
          this.displayConfig = (result as any).display || RunbookPanel.readDisplaySettings();
          this.gertCwd = restartCwd;

          // Reset all state — restore original creation args
          this.creationArgs = { ...args };
          this.steps = result.steps || [];
          this.tree = (result as any).tree || [];
          this.runbookKind = (result as any).kind || 'mitigation';
          this.prose = (result as any).prose || null;
          this.runbookDescription = (result as any).description || '';
          this.currentStepDetail = null;
          this.outcomeResult = null;
          this.iterateState = null;
          this.pendingNextRunbook = null;
          this.runCompleted = false;
          this.viewingStepId = null;
          this.stepHistory = [];
          this.chainHistory = [];
          this.chainBaseDirs = [];
          this.stepStates.clear();
          this.stepDetails.clear();
          this.stepErrors.clear();
          this.captures = {};
          for (const step of this.steps) {
            this.stepStates.set(step.id, 'pending');
          }
          initTreeStates(this.tree, this.stepStates);
          this.processing = false;
          this.updateWebview();

          // Auto-start first step
          await newClient.execNext();
        } catch (e: any) {
          this.processing = false;
          this.updateWebview();
          vscode.window.showErrorMessage(`Gert: Restart failed — ${e.message}`);
        }
        break;
      }

      case 'copySummary': {
        if (msg.text) {
          vscode.env.clipboard.writeText(msg.text).then(() => {
            vscode.window.showInformationMessage('Gert: Summary copied to clipboard — paste into discussion, email, or Teams.');
          });
        }
        break;
      }

      case 'openExternal': {
        break;
      }

      case 'saveForReplay': {
        const path = require('path');
        const fs = require('fs');

        // Use the ORIGINAL (root) runbook path for save location, not the last child in a chain
        const runbookPath = this.originalCreationArgs?.runbookPath || this.creationArgs?.runbookPath || '';
        if (!runbookPath) {
          vscode.window.showErrorMessage('Gert: No runbook path — cannot save scenario.');
          break;
        }

        // Derive folder: scenarios/{runbook-name}/{scenario-name}
        const tsgName = path.basename(runbookPath).replace(/\.runbook\.(yaml|yml)$/i, '');
        const outcomeSuffix = this.outcomeResult?.state || 'completed';
        const defaultName = `${tsgName}-${outcomeSuffix}`;
        const scenariosBase = path.join(path.dirname(runbookPath), 'scenarios', tsgName);

        // Auto-increment if folder exists: name, name-1, name-2, ...
        let outputDir = path.join(scenariosBase, defaultName);
        if (fs.existsSync(outputDir)) {
          let i = 1;
          while (fs.existsSync(path.join(scenariosBase, `${defaultName}-${i}`))) { i++; }
          outputDir = path.join(scenariosBase, `${defaultName}-${i}`);
        }

        try {
          fs.mkdirSync(outputDir, { recursive: true });

          // 1. Write inputs.yaml from resolved vars
          const vars = this.creationArgs?.vars || {};
          if (Object.keys(vars).length > 0) {
            const lines = Object.entries(vars)
              .sort(([a], [b]) => a.localeCompare(b))
              .map(([k, v]) => `${k}: "${String(v).replace(/"/g, '\\"')}"`)
              .join('\n') + '\n';
            fs.writeFileSync(path.join(outputDir, 'inputs.yaml'), lines, 'utf-8');
          }

          // 2. Copy step response JSON files from ALL run baseDirs (parent + child chains)
          const allBaseDirs = this.chainBaseDirs.length > 0
            ? this.chainBaseDirs
            : (this.runBaseDir ? [this.runBaseDir] : []);
          let copied = 0;
          const dstSteps = path.join(outputDir, 'steps');
          for (const baseDir of allBaseDirs) {
            const absBaseDir = path.isAbsolute(baseDir)
              ? baseDir
              : path.join(this.gertCwd, baseDir);
            const srcSteps = path.join(absBaseDir, 'steps');
            if (fs.existsSync(srcSteps)) {
              fs.mkdirSync(dstSteps, { recursive: true });
              const entries = fs.readdirSync(srcSteps) as string[];
              for (const entry of entries) {
                if (entry.endsWith('.json')) {
                  fs.copyFileSync(path.join(srcSteps, entry), path.join(dstSteps, entry));
                  copied++;
                }
              }
            }
          }

          vscode.window.showInformationMessage(
            `Gert: Saved → ${outputDir} (${Object.keys(vars).length} inputs, ${copied} steps)`,
            'Save as Test Case'
          ).then(choice => {
            if (choice === 'Save as Test Case') {
              this.generateTestYaml(outputDir);
            }
          });
        } catch (e: any) {
          vscode.window.showErrorMessage(`Gert: Failed to save scenario — ${e.message}`);
        }
        break;
      }
    }
  }
  private updateWebview() {
    if (this.disposed) return;
    this.panel.webview.html = this.getHtml();
  }

  /**
   * Generate the webview HTML.
   */
  private getHtml(): string {
    // Build step number map: stepId → "N.M" for executed steps across the chain
    const showNumbers = vscode.workspace.getConfiguration('gert').get<boolean>('showStepNumbers', true);
    const stepNumberMap = new Map<string, string>();
    let tsgIdx = 1;
    for (const entry of this.chainHistory) {
      let stepNum = 1;
      for (const s of entry.steps) {
        if (s.state === 'passed' || s.state === 'failed') {
          stepNumberMap.set(s.id, `${tsgIdx}.${stepNum}`);
          stepNum++;
        }
      }
      tsgIdx++;
    }
    // Current TSG steps
    const currentSteps = this.steps.length > 0 ? this.steps : this.collectTreeSteps(this.tree);
    let currentStepNum = 1;
    for (const s of currentSteps) {
      const state = this.stepStates.get(s.id);
      if (state === 'passed' || state === 'failed' || state === 'running') {
        stepNumberMap.set(s.id, `${tsgIdx}.${currentStepNum}`);
        currentStepNum++;
      }
    }

    // Render tree if available, otherwise flat steps
    const currentTreeHtml = this.tree.length > 0
      ? this.renderTree(this.tree, 0, undefined, stepNumberMap)
      : this.renderFlatSteps();

    // Build unified workflow map with chain history — one continuous growing tree
    let workflowHtml = '';
    const currentName = require('path').basename(this.creationArgs?.runbookPath || '').replace(/\.runbook\.(yaml|yml)$/i, '');

    if (this.chainHistory.length > 0) {
      let tsgNum = 1;
      for (const entry of this.chainHistory) {
        const tsgLabel = showNumbers ? `${tsgNum}. ` : '';
        workflowHtml += `
          <div class="tsg-header tsg-completed">
            <span class="tsg-icon">📋</span>
            <span class="tsg-name">${tsgLabel}${escapeHtml(entry.name)}</span>
            <span class="tsg-status">${escapeHtml(entry.outcome ? (entry.outcome.isRouting ? '→ continued' : outcomeHeadline(entry.outcome.state)) : '')}</span>
          </div>`;
        if (entry.tree && entry.tree.length > 0) {
          workflowHtml += this.renderTree(entry.tree, 0, entry.stepStates, stepNumberMap, entry.outcome);
        }
        tsgNum++;
      }

      const currentLabel = showNumbers ? `${tsgNum}. ` : '';
      workflowHtml += `
        <div class="tsg-header tsg-current">
          <span class="tsg-icon">▶</span>
          <span class="tsg-name">${currentLabel}${escapeHtml(currentName)}</span>
          <span class="tsg-status">(current)</span>
        </div>`;
    }

    workflowHtml += currentTreeHtml;

    const chainBreadcrumb = ''; // chain is now shown inline in the workflow map

    // Outcome banner at top if reached
    const outcomeBanner = this.outcomeResult
      ? `<div class="outcome-banner ${this.pendingNextRunbook ? 'routed' : this.outcomeResult.state}">
          <div class="outcome-state">■ ${this.pendingNextRunbook ? 'CONTINUE TO NEXT TSG' : outcomeHeadline(this.outcomeResult.state)}</div>
          <div class="outcome-recommendation">${escapeHtml(this.outcomeResult.recommendation || '')}</div>
        </div>`
      : '';

    // Active step detail
    let activeStepHtml = '';
    const detail = this.currentStepDetail;
    const runDone = !!this.outcomeResult || this.runCompleted;
    const isReplay = this.creationArgs?.mode === 'replay';
    const freeNav = this.runbookKind !== 'mitigation' || runDone || isReplay;
    const browsedStep = this.viewingStepId ? this.findStepDetail(this.viewingStepId) : null;

    if (browsedStep && (this.outcomeResult || this.runCompleted || this.viewingStepId !== detail?.stepId)) {
      // Browsing a step — show its detail regardless of outcome
      const bId = browsedStep.stepId || browsedStep.id;
      const bState = this.stepStates.get(bId) || 'pending';
      const returnTarget = this.outcomeResult ? 'outcome' : 'active step';
      activeStepHtml = `
        ${!this.outcomeResult ? `<div class="browsing-banner" onclick="returnToActive()">← Return to ${returnTarget}</div>` : ''}
        <div class="active-step-header">
          <span class="type-badge ${browsedStep.type}">${browsedStep.type.toUpperCase()}</span>
          <span class="step-name">${browsedStep.title || bId}</span>
          <span class="state-pill ${bState}">${bState}</span>
          <span class="viewing-badge">VIEWING</span>
        </div>
        <div class="step-id">${bId}</div>
        ${browsedStep.query ? `<div class="query-block"><div class="query-header">${browsedStep.queryType || 'query'}</div><pre class="query-code">${highlightQuery(browsedStep.query, browsedStep.queryType)}</pre></div>` : ''}
        ${browsedStep.command ? `<div class="query-block"><div class="query-header">command</div><pre class="query-code">${escapeHtml(browsedStep.command)}</pre></div>` : ''}
        ${browsedStep.tool ? this.renderToolInfo(browsedStep.tool) : ''}
        ${browsedStep.instructions ? `<div class="instructions">${this.renderMarkdownish(browsedStep.instructions)}</div>` : '<div class="no-step">No instructions for this step.</div>'}`;
    } else if (this.outcomeResult) {
      const summaryText = this.buildSummaryText();
      const hl = this.pendingNextRunbook ? 'CONTINUE TO NEXT TSG' : outcomeHeadline(this.outcomeResult.state);
      const badge = this.pendingNextRunbook ? 'ROUTING' : (outcomeIsConclusion(this.outcomeResult.state) ? 'RESULT' : 'RECOMMENDATION');
      activeStepHtml = `
        <div class="outcome-detail">
          <div class="active-step-header">
            <span class="type-badge outcome">${badge}</span>
            <span class="step-name">${hl}</span>
          </div>
          ${detail?.instructions ? `<div class="instructions" style="margin:12px 0">${this.renderMarkdownish(detail.instructions)}</div>` : ''}
          ${this.chainHistory.length > 0 ? (() => {
          const showNums = vscode.workspace.getConfiguration('gert').get<boolean>('showStepNumbers', true);
          return `
          <div class="chain-summary" style="margin:12px 0;padding:10px;background:var(--vscode-editor-background);border:1px solid var(--vscode-panel-border);border-radius:4px;font-size:12px">
            <div style="font-weight:bold;margin-bottom:8px">Execution Path (${this.chainHistory.length + 1} TSGs)</div>
            ${this.chainHistory.map((e, i) => `
              <div style="margin-bottom:6px;padding-left:${i * 12}px">
                <span style="color:var(--vscode-charts-green)">✓</span>
                <strong>${showNums ? `${i + 1}. ` : ''}${escapeHtml(e.name)}</strong>
                ${e.outcome ? ` → ${escapeHtml(e.outcome.isRouting ? 'continued' : outcomeHeadline(e.outcome.state))}` : ''}
                ${Object.keys(e.captures).length > 0 ? `<div style="opacity:0.7;padding-left:16px">${
                  Object.entries(e.captures)
                    .filter(([, v]) => v && v !== '[]' && v !== '{}')
                    .slice(0, 4)
                    .map(([k, v]) => `${k}=${v}`)
                    .join(', ')
                }</div>` : ''}
              </div>
            `).join('')}
            <div style="margin-bottom:4px;padding-left:${this.chainHistory.length * 12}px">
              <span style="color:var(--vscode-charts-yellow)">■</span>
              <strong>${showNums ? `${this.chainHistory.length + 1}. ` : ''}${escapeHtml(require('path').basename(this.creationArgs?.runbookPath || '').replace(/\.runbook\.(yaml|yml)$/i, ''))}</strong>
              → ${escapeHtml(hl)}
            </div>
          </div>
          `; })() : ''}
          <div class="outcome-recommendation-full">${escapeHtml(this.outcomeResult.recommendation || '')}</div>
          ${this.pendingNextRunbook ? `<div class="next-runbook-banner" style="margin-top:12px;padding:10px;background:var(--vscode-editor-background);border:1px solid var(--vscode-focusBorder);border-radius:4px">
            <div style="font-weight:bold;margin-bottom:6px">Next TSG: ${escapeHtml(this.pendingNextRunbook.file)}</div>
            <button class="btn btn-primary" onclick="chainToRunbook()">Continue to TSG →</button>
          </div>` : ''}
          <div class="actions" style="margin-top:16px">
            ${this.showCopySummary() ? '<button class="btn btn-primary" onclick="copySummary()">📋 Copy Summary</button>' : ''}
            ${this.showSaveForReplay() && this.creationArgs?.mode !== 'replay' ? '<button class="btn btn-secondary" onclick="saveForReplay()" style="margin-left:8px">💾 Save for Replay</button>' : ''}
          </div>
          <textarea id="summaryText" style="position:absolute;left:-9999px">${escapeHtml(summaryText)}</textarea>
        </div>`;
    } else if (this.runCompleted) {
      const completeSummary = this.buildSummaryText();
      activeStepHtml = `
        <div class="outcome-detail">
          <div class="active-step-header">
            <span class="type-badge outcome">DONE</span>
            <span class="step-name">Runbook Complete</span>
          </div>
          <div class="outcome-recommendation-full">All steps have been completed.</div>
          <div class="actions" style="margin-top:16px">
            ${this.showCopySummary() ? '<button class="btn btn-primary" onclick="copySummary()">📋 Copy Summary</button>' : ''}
            ${this.showSaveForReplay() && this.creationArgs?.mode !== 'replay' ? '<button class="btn btn-secondary" onclick="saveForReplay()" style="margin-left:8px">💾 Save for Replay</button>' : ''}
          </div>
          <textarea id="summaryText" style="position:absolute;left:-9999px">${escapeHtml(completeSummary)}</textarea>
        </div>`;
    } else if (this.processing) {
      activeStepHtml = `
        <div class="processing-indicator">
          <div class="spinner"></div>
          <div class="processing-text">Executing step...</div>
        </div>`;
    } else if (detail) {
      const stepState = this.stepStates.get(detail.stepId) || 'running';
      const isManual = detail.type === 'manual';
      const isRunning = stepState === 'running' && !isManual;
      activeStepHtml = `
        <div class="active-step-header">
          <span class="type-badge ${detail.type}">${detail.type.toUpperCase()}</span>
          <span class="step-name">${detail.title || detail.stepId}</span>
          <span class="state-pill ${stepState}">${stepState}</span>
        </div>
        <div class="step-id">${detail.stepId}</div>
        ${isRunning ? `<div class="executing-indicator"><div class="spinner"></div><div class="processing-text">Executing ${detail.type}${detail.tool ? ` ${detail.tool.name}.${detail.tool.action}` : ''} step...</div></div>` : ''}
        ${detail.query ? `<div class="query-block"><div class="query-header">${detail.queryType || 'query'}</div><pre class="query-code">${highlightQuery(detail.query, detail.queryType)}</pre></div>` : ''}
        ${detail.command ? `<div class="query-block"><div class="query-header">command</div><pre class="query-code">${escapeHtml(detail.command)}</pre></div>` : ''}
        ${detail.tool ? this.renderToolInfo(detail.tool) : ''}
        ${detail.instructions ? `<div class="instructions">${this.renderMarkdownish(detail.instructions)}</div>` : ''}
        ${detail.outcomes ? this.renderOutcomes(detail.outcomes, stepState) : ''}`;
    } else {
      activeStepHtml = `<div class="no-step">Starting execution...</div>`;
    }

    // Captures section (gated by display config)
    const captureEntries = Object.entries(this.captures);
    const capturesHtml = this.showCaptures() && captureEntries.length > 0
      ? `<div class="captures-section">
          <h3>Captures</h3>
          ${captureEntries.map(([k, v]) =>
            `<div class="capture"><span class="key">${escapeHtml(k)}</span><span class="value">${formatCaptureValue(String(v))}</span></div>`
          ).join('')}
        </div>`
      : '';

    // Inputs summary (collapsible) — shows current input values so operator can spot bad values
    const inputNames = Object.keys(this.creationArgs?.vars || {});
    const inputsSummaryHtml = inputNames.length > 0
      ? `<div class="inputs-summary">
          <div class="inputs-toggle" onclick="toggleInputs()">
            <span class="arrow" id="inputsArrow">▶</span>
            <span>Inputs (${inputNames.length})</span>
          </div>
          <div class="inputs-body" id="inputsBody" style="display:none">
            ${inputNames.map(name => {
              const val = this.creationArgs!.vars[name] || '';
              const isEmpty = !val.trim();
              return `<div class="input-row">
                <span class="input-name">${escapeHtml(name)}</span>
                <span class="input-value${isEmpty ? ' empty' : ''}">${isEmpty ? '(empty)' : escapeHtml(val)}</span>
              </div>`;
            }).join('')}
          </div>
        </div>`
      : '';

    return `<!DOCTYPE html>
<html>
<head>
<style>
  :root {
    --bg: var(--vscode-editor-background);
    --fg: var(--vscode-editor-foreground);
    --border: var(--vscode-panel-border);
    --accent: var(--vscode-focusBorder);
    --success: #4ec9b0;
    --error: #f44747;
    --warning: #cca700;
    --muted: var(--vscode-descriptionForeground);
  }
  body { margin: 0; padding: 0; font-family: var(--vscode-font-family); color: var(--fg); background: var(--bg); display: flex; height: 100vh; }
  .processing-indicator { display: flex; flex-direction: column; align-items: center; justify-content: center; padding: 40px 0; gap: 12px; }
  .spinner { width: 28px; height: 28px; border: 3px solid var(--border); border-top-color: var(--accent); border-radius: 50%; animation: spin 0.8s linear infinite; }
  @keyframes spin { to { transform: rotate(360deg); } }
  .processing-text { color: var(--muted); font-size: 12px; }
  .executing-indicator { display: flex; align-items: center; gap: 10px; padding: 12px 0; margin: 8px 0; border: 1px solid var(--border); border-radius: 4px; padding-left: 12px; background: rgba(78, 201, 176, 0.05); }
  .executing-indicator .spinner { width: 18px; height: 18px; flex-shrink: 0; }
  .layout { display: flex; width: 100%; height: 100%; }
  .prose-panel { flex: 0 0 auto; width: 40%; min-width: 150px; overflow-y: auto; padding: 12px; }
  .splitter-left { flex: 0 0 5px; cursor: col-resize; background: var(--border); position: relative; z-index: 10; transition: background 0.15s; }
  .splitter-left:hover, .splitter-left.dragging { background: var(--accent); }
  .workflow-map { flex: 0 0 auto; width: 260px; min-width: 160px; overflow-y: auto; padding: 12px; }
  .splitter { flex: 0 0 5px; cursor: col-resize; background: var(--border); position: relative; z-index: 10; transition: background 0.15s; }
  .splitter:hover, .splitter.dragging { background: var(--accent); }
  .active-step-panel { flex: 1; min-width: 250px; overflow-y: auto; padding: 12px; }

  .workflow-header { font-size: 11px; text-transform: uppercase; letter-spacing: 1px; color: var(--muted); margin-bottom: 8px; display: flex; align-items: center; gap: 6px; }
  .tsg-header { padding: 8px 12px; margin: 8px 0 4px 0; border-radius: 4px; display: flex; align-items: center; gap: 8px; font-size: 13px; font-weight: bold; }
  .tsg-header.tsg-completed { background: rgba(0,200,0,0.1); border: 1px solid rgba(0,200,0,0.3); }
  .tsg-header.tsg-current { background: rgba(0,120,212,0.15); border: 1px solid rgba(0,120,212,0.4); }
  .tsg-icon { font-size: 16px; }
  .tsg-name { flex: 1; }
  .tsg-status { font-size: 11px; font-weight: normal; opacity: 0.7; }
  .tab-bar { display: flex; gap: 2px; }
  .tab-btn { padding: 4px 12px; border: none; border-radius: 3px 3px 0 0; cursor: pointer; font-size: 11px; text-transform: uppercase; letter-spacing: 1px; background: transparent; color: var(--muted); }
  .tab-btn.active { background: var(--vscode-tab-activeBackground, rgba(255,255,255,0.1)); color: var(--vscode-foreground); font-weight: bold; }
  .tab-btn:hover:not(.active) { background: rgba(255,255,255,0.05); }
  .prose-content { padding: 12px 16px; overflow-y: auto; line-height: 1.6; }
  .prose-content h1 { font-size: 1.4em; border-bottom: 1px solid var(--vscode-panel-border); padding-bottom: 6px; }
  .prose-content h2 { font-size: 1.2em; margin-top: 20px; color: var(--vscode-textLink-foreground); }
  .prose-content h3 { font-size: 1.05em; margin-top: 14px; }
  .prose-content pre { background: var(--vscode-textCodeBlock-background); padding: 10px; border-radius: 4px; overflow-x: auto; font-family: var(--vscode-editor-font-family, monospace); font-size: 12px; }
  .prose-content code { font-family: var(--vscode-editor-font-family, monospace); font-size: 12px; }
  .prose-content blockquote { border-left: 3px solid var(--vscode-textBlockQuote-border); padding: 6px 12px; margin: 8px 0; opacity: 0.85; }
  .prose-content img { max-width: 100%; border: 1px solid var(--vscode-panel-border); border-radius: 4px; }
  .prose-content section[data-step-id] { border-left: 3px solid transparent; padding-left: 10px; margin: 6px 0; transition: border-color 0.3s, background 0.3s; }
  .prose-content section[data-step-id].active { border-left-color: var(--vscode-focusBorder); background: rgba(0,120,212,0.08); }
  .hljs-keyword { color: #569cd6; } .hljs-built_in { color: #dcdcaa; } .hljs-string { color: #ce9178; } .hljs-number { color: #b5cea8; } .hljs-comment { color: #6a9955; font-style: italic; } .hljs-operator { color: #d4d4d4; font-weight: bold; } .hljs-variable { color: #4ec9b0; } .hljs-type { color: #4ec9b0; font-weight: bold; }
  .workflow-header .badge { background: var(--accent); color: var(--bg); padding: 1px 6px; border-radius: 3px; font-size: 10px; }
  .workflow-header .badge.guide { background: #6a9955; }
  .workflow-header .badge.rca { background: #b180d7; }
  .workflow-header .badge.reference { background: #569cd6; }
  .workflow-header .badge.composable { background: #d7ba7d; color: #1e1e1e; }
  .workflow-header .badge.mitigation { background: #f44747; }
  .workflow-header .badge.replay { background: #264f78; color: #9cdcfe; font-weight: normal; }

  /* Outcome banner */
  .outcome-banner { padding: 10px 12px; border-radius: 4px; margin-bottom: 12px; border-left: 3px solid; }
  .outcome-banner.no_action { border-color: var(--success); background: rgba(78, 201, 176, 0.1); }
  .outcome-banner.resolved { border-color: var(--success); background: rgba(78, 201, 176, 0.1); }
  .outcome-banner.escalated { border-color: var(--warning); background: rgba(204, 167, 0, 0.1); }
  .outcome-banner.needs_rca { border-color: var(--accent); background: rgba(0, 122, 204, 0.1); }
  .outcome-state { font-weight: bold; font-size: 13px; margin-bottom: 4px; }
  .outcome-recommendation { font-size: 12px; white-space: pre-wrap; }

  /* Steps */
  .step { display: flex; align-items: center; gap: 8px; padding: 6px 8px; border-radius: 4px; cursor: pointer; font-size: 13px; }
  .step:hover { background: var(--vscode-list-hoverBackground); }
  .step.current { background: var(--vscode-list-activeSelectionBackground); color: var(--vscode-list-activeSelectionForeground); }
  .step.passed .state-icon { color: var(--success); }
  .step.failed .state-icon { color: var(--error); }
  .step.skipped { opacity: 0.35; }
  .step.skipped .step-title { text-decoration: line-through; text-decoration-color: rgba(128,128,128,0.4); }
  .step.running .state-icon { color: var(--accent); }
  .step.pending .state-icon { color: var(--muted); }
  .state-icon { font-size: 14px; width: 18px; text-align: center; }
  .type-icon { font-size: 12px; width: 18px; text-align: center; opacity: 0.7; }
  .step-title { flex: 1; }
  .when-badge { font-size: 10px; padding: 1px 4px; border-radius: 2px; background: var(--vscode-badge-background); color: var(--vscode-badge-foreground); }

  .step-error { font-size: 11px; color: var(--error); padding: 2px 8px 4px 44px; }

  /* Tree elements */
  .tree-outcome { display: flex; align-items: center; gap: 6px; padding: 3px 0; font-size: 12px; }
  .outcome-arrow { color: var(--muted); font-family: monospace; }
  .outcome-state-label { font-weight: bold; font-size: 11px; padding: 1px 6px; border-radius: 3px; }
  .outcome-state-label.no_action { color: var(--success); background: rgba(78,201,176,0.15); }
  .outcome-state-label.resolved { color: var(--success); background: rgba(78,201,176,0.15); }
  .outcome-state-label.escalated { color: var(--warning); background: rgba(204,167,0,0.15); }
  .outcome-state-label.needs_rca { color: var(--accent); background: rgba(0,122,204,0.15); }
  .outcome-state-label.skipped { color: var(--muted); background: rgba(128,128,128,0.15); }
  .tree-outcome.skipped { opacity: 0.35; }
  .outcome-when { font-size: 10px; color: var(--muted); font-family: var(--vscode-editor-font-family); }

  .branch { margin: 2px 0; }
  .branch-header { display: flex; align-items: center; gap: 6px; padding: 4px 0; font-size: 12px; }
  .branch-connector { color: var(--accent); font-family: monospace; font-weight: bold; }
  .branch-label { color: var(--accent); font-weight: 500; }
  .branch.skipped { opacity: 0.35; }
  .branch.skipped .branch-connector { color: var(--muted); }
  .branch.skipped .branch-label { color: var(--muted); font-style: italic; }

  /* Active step */
  .active-step-header { display: flex; align-items: center; gap: 8px; margin-bottom: 8px; flex-wrap: wrap; }
  .type-badge { padding: 2px 8px; border-radius: 3px; font-size: 11px; font-weight: bold; display: inline-block; }
  .type-badge.cli { background: #4e3a1a; color: #dcdcaa; }
  .type-badge.manual { background: #3a4e2a; color: #b5cea8; }
  .type-badge.tool { background: #3a2a4e; color: #c9a8e8; }
  .type-badge.invoke { background: #2a3a4e; color: #a8c8e8; }
  .type-badge.outcome { background: #264f78; color: #9cdcfe; }
  .step-name { font-size: 14px; font-weight: 500; }
  .step-id { font-size: 11px; color: var(--muted); margin-bottom: 12px; }
  .state-pill { font-size: 10px; padding: 1px 6px; border-radius: 8px; }
  .state-pill.passed { background: rgba(78, 201, 176, 0.2); color: var(--success); }
  .state-pill.failed { background: rgba(244, 71, 71, 0.2); color: var(--error); }
  .state-pill.running { background: rgba(0, 122, 204, 0.2); color: var(--accent); }
  .no-step { color: var(--muted); font-style: italic; padding: 20px 0; }

  /* Instructions — rendered Markdown */
  .instructions { font-size: 12px; line-height: 1.6; padding: 8px; background: var(--vscode-textCodeBlock-background); border-radius: 4px; margin-bottom: 12px; }
  .instructions h1, .instructions h2, .instructions h3 { margin: 12px 0 6px; font-size: 14px; color: var(--fg); }
  .instructions h2 { font-size: 13px; }
  .instructions h3 { font-size: 12px; }
  .instructions p { margin: 4px 0; }
  .instructions table { border-collapse: collapse; width: 100%; margin: 8px 0; font-size: 12px; }
  .instructions th, .instructions td { border: 1px solid var(--border); padding: 4px 8px; text-align: left; }
  .instructions th { background: rgba(255,255,255,0.05); font-weight: 600; }
  .instructions code { background: rgba(255,255,255,0.08); padding: 1px 4px; border-radius: 3px; font-size: 11px; }
  .instructions pre { background: var(--vscode-textCodeBlock-background); padding: 8px; border-radius: 4px; overflow-x: auto; margin: 8px 0; }
  .instructions pre code { background: none; padding: 0; }
  .instructions ul, .instructions ol { margin: 4px 0; padding-left: 20px; }
  .instructions a { color: var(--accent); }
  .instructions img { max-width: 100%; margin: 8px 0; border-radius: 4px; }
  .instructions strong { color: #dcdcaa; }
  .query-block { margin-bottom: 12px; border: 1px solid var(--vscode-editorWidget-border, #444); border-radius: 4px; overflow: hidden; }
  .query-header { font-size: 10px; text-transform: uppercase; letter-spacing: 1px; padding: 4px 8px; background: var(--vscode-editorWidget-background, #252526); color: var(--muted); border-bottom: 1px solid var(--vscode-editorWidget-border, #444); }
  .query-code { font-size: 11px; line-height: 1.4; padding: 8px; margin: 0; background: var(--vscode-textCodeBlock-background); color: var(--vscode-editor-foreground); white-space: pre-wrap; word-break: break-word; font-family: var(--vscode-editor-font-family, monospace); }

  /* Tool info block */
  .tool-info { margin-bottom: 12px; border: 1px solid var(--vscode-editorWidget-border, #444); border-radius: 4px; overflow: hidden; }
  .tool-header { display: flex; align-items: center; gap: 6px; padding: 6px 8px; background: var(--vscode-editorWidget-background, #252526); font-size: 12px; }
  .tool-label { font-size: 10px; text-transform: uppercase; letter-spacing: 1px; color: var(--muted); }
  .tool-name { color: #c9a8e8; font-weight: 600; }
  .tool-dot { color: var(--muted); }
  .tool-action { color: #dcdcaa; }
  .tool-gov-badge { font-size: 10px; padding: 1px 6px; border-radius: 8px; margin-left: auto; }
  .tool-gov-badge.readonly { background: rgba(78, 201, 176, 0.15); color: #4ec9b0; }
  .tool-gov-badge.approval { background: rgba(244, 178, 71, 0.15); color: #f4b247; }
  .tool-args { padding: 6px 8px; background: var(--vscode-textCodeBlock-background); }
  .tool-args-header { font-size: 10px; text-transform: uppercase; letter-spacing: 1px; color: var(--muted); margin-bottom: 4px; }
  .tool-arg-row { display: flex; gap: 8px; font-size: 11px; line-height: 1.6; font-family: var(--vscode-editor-font-family, monospace); }
  .tool-arg-name { color: #9cdcfe; min-width: 80px; }
  .tool-arg-value { color: #ce9178; }
  /* highlight.js VS Code Dark+ theme */
  .query-code .hljs-keyword { color: #569cd6; }
  .query-code .hljs-built_in { color: #dcdcaa; }
  .query-code .hljs-type { color: #4ec9b0; }
  .query-code .hljs-literal { color: #569cd6; }
  .query-code .hljs-number { color: #b5cea8; }
  .query-code .hljs-string { color: #ce9178; }
  .query-code .hljs-comment { color: #6a9955; font-style: italic; }
  .query-code .hljs-operator { color: #d4d4d4; }
  .query-code .hljs-punctuation { color: #d4d4d4; }
  .query-code .hljs-variable { color: #9cdcfe; }
  .query-code .hljs-title { color: #dcdcaa; }
  .query-code .hljs-function { color: #dcdcaa; }
  .query-code .hljs-params { color: #9cdcfe; }

  /* Outcomes list */
  .outcomes-section { margin-top: 12px; border-top: 1px solid var(--border); padding-top: 8px; }
  .outcomes-section h3 { font-size: 11px; text-transform: uppercase; letter-spacing: 1px; color: var(--muted); margin: 0 0 6px; }
  .outcome-item { font-size: 12px; padding: 4px 0; display: flex; gap: 6px; }
  .outcome-item .state-label { font-weight: bold; min-width: 80px; }
  .outcome-item .state-label.resolved { color: var(--success); }
  .outcome-item .state-label.escalated { color: var(--warning); }
  .outcome-item .state-label.needs_rca { color: var(--accent); }
  .outcome-item .state-label.no_action { color: var(--muted); }
  .outcome-item .when-expr { color: var(--muted); font-family: var(--vscode-editor-font-family); font-size: 11px; }
  .outcome-rec-preview { font-size: 11px; color: var(--muted); padding-left: 86px; margin-bottom: 4px; }

  /* Captures */
  .captures-section { margin-top: 12px; border-top: 1px solid var(--border); padding-top: 8px; }
  .captures-section h3 { font-size: 11px; text-transform: uppercase; letter-spacing: 1px; color: var(--muted); margin: 0 0 6px; }
  .capture { display: flex; gap: 8px; font-size: 12px; padding: 2px 0; font-family: var(--vscode-editor-font-family); }
  .capture .key { color: var(--success); min-width: 120px; }
  .capture .value { color: var(--fg); word-break: break-all; }
  .capture-count { color: var(--accent); font-weight: bold; }
  .capture-json { font-size: 10px; margin: 4px 0 0; padding: 4px 8px; background: var(--vscode-textCodeBlock-background); border-radius: 3px; white-space: pre-wrap; max-height: 150px; overflow-y: auto; }

  /* Outcome detail */
  .outcome-recommendation-full { font-size: 13px; white-space: pre-wrap; line-height: 1.5; margin-top: 12px; }

  .actions { margin-top: 16px; display: flex; gap: 8px; }
  .btn { padding: 6px 14px; border: none; border-radius: 3px; cursor: pointer; font-size: 12px; }
  .btn-sm { padding: 3px 10px; font-size: 11px; }
  .btn-primary { background: var(--accent); color: var(--vscode-button-foreground, #fff); }
  .btn-secondary { background: var(--vscode-button-secondaryBackground); color: var(--vscode-button-secondaryForeground); }
  .btn-resolved { background: #264f3a; color: var(--success); border: 1px solid var(--success); }
  .btn-escalated { background: #4e3a1a; color: var(--warning); border: 1px solid var(--warning); }
  .btn-resolved:hover { background: #2e6b48; }
  .btn-escalated:hover { background: #6b4f22; }
  .btn:hover { opacity: 0.9; }
  .outcome-choice { margin-top: 16px; }
  .outcome-choice-label { font-size: 11px; text-transform: uppercase; letter-spacing: 1px; color: var(--muted); margin-bottom: 8px; }

  /* Navigation */
  .nav-btn { background: none; border: 1px solid var(--border); color: var(--fg); padding: 2px 8px; border-radius: 3px; cursor: pointer; font-size: 13px; margin-left: auto; }
  .nav-btn:hover { background: var(--vscode-list-hoverBackground); }
  .viewing-badge { background: var(--warning); color: #000; padding: 1px 6px; border-radius: 3px; font-size: 10px; font-weight: bold; }
  .browsing-banner { padding: 6px 10px; margin-bottom: 8px; background: rgba(204, 167, 0, 0.1); border: 1px solid var(--warning); border-radius: 4px; font-size: 12px; color: var(--warning); cursor: pointer; }
  .browsing-banner:hover { background: rgba(204, 167, 0, 0.2); }
  .step.clickable { cursor: pointer; }
  .step.clickable:hover .step-title { text-decoration: underline; }

  /* Inputs summary */
  .inputs-summary { margin-bottom: 12px; border: 1px solid var(--border); border-radius: 4px; overflow: hidden; }
  .inputs-toggle { display: flex; align-items: center; gap: 6px; padding: 6px 10px; cursor: pointer; font-size: 11px; text-transform: uppercase; letter-spacing: 1px; color: var(--muted); background: var(--vscode-sideBar-background, transparent); user-select: none; }
  .inputs-toggle:hover { background: var(--vscode-list-hoverBackground); }
  .inputs-toggle .arrow { transition: transform 0.15s; display: inline-block; }
  .inputs-toggle .arrow.open { transform: rotate(90deg); }
  .inputs-body { padding: 4px 10px 8px; }
  .input-row { display: flex; gap: 8px; font-size: 12px; padding: 2px 0; font-family: var(--vscode-editor-font-family); }
  .input-row .input-name { color: var(--accent); min-width: 140px; }
  .input-row .input-value { color: var(--fg); word-break: break-all; }
  .input-row .input-value.empty { color: var(--error); font-style: italic; }
</style>
</head>
<body>
${chainBreadcrumb}
<div class="layout">
  <div class="prose-panel" id="prosePanel" style="width:${this.proseWidth}">
    <div class="workflow-header"><span>TSG PROSE</span></div>
    <div class="prose-content">${this.getProseHtml()}</div>
  </div>
  <div class="splitter-left" id="splitterLeft"></div>
  <div class="workflow-map" id="workflowMap" style="width:${this.mapWidth}">
    <div class="workflow-header">
      <span>WORKFLOW MAP</span>
      <span class="badge ${this.runbookKind}">${this.runbookKind.toUpperCase()}</span>
      ${freeNav ? '<button class="nav-btn" onclick="restart()" title="Restart from beginning">⟲ Restart</button>' : ''}
    </div>
    ${outcomeBanner}
    ${workflowHtml}
  </div>
  <div class="splitter" id="splitter"></div>
  <div class="active-step-panel">
    <div class="workflow-header">
      ${freeNav && this.stepHistory.length > 0 ? '<button class="nav-btn" onclick="backStep()" title="Back" style="margin-left:0;margin-right:8px">←</button>' : ''}
      <span>${this.outcomeResult ? 'OUTCOME' : this.runCompleted ? 'COMPLETE' : browsedStep ? 'STEP DETAIL' : 'ACTIVE STEP'}</span>
      ${isReplay && !this.outcomeResult && !this.runCompleted ? (
        this.autoRunning
          ? '<button class="btn btn-secondary btn-sm" onclick="stopRunAll()" style="margin-left:auto" title="Stop auto-replay">⏹ Stop</button>'
          : '<button class="btn btn-primary btn-sm" onclick="runAll()" style="margin-left:auto" title="Replay all steps automatically">▶ Run All</button>'
      ) : ''}
    </div>
    ${inputsSummaryHtml}
    ${activeStepHtml}
    ${capturesHtml}
    ${!this.outcomeResult && !this.runCompleted && !(browsedStep && this.viewingStepId !== detail?.stepId) ? (() => {
      const isManual = detail?.type === 'manual';      const hasOutcomes = detail?.outcomes && detail.outcomes.length > 0;
      const stepState = detail ? (this.stepStates.get(detail.stepId) || 'running') : 'running';
      const busy = this.processing || this.autoRunning || (stepState === 'running' && !isManual);
      if (busy) {
        return ''; // hide all buttons while busy
      }
      const disabledAttr = '';
      if (isManual && hasOutcomes) {
        // Show outcome choice buttons (no generic "Continue")
        const buttons = detail.outcomes.map((o: any) => {
          const cls = o.state === 'resolved' ? 'btn-resolved' : o.state === 'escalated' ? 'btn-escalated' : 'btn-secondary';
          return `<button class="btn ${cls}" onclick="chooseOutcome('${escapeHtml(detail.stepId)}', '${o.state}')">${outcomeButtonLabel(o.state)}</button>`;
        }).join('');
        return `<div class="outcome-choice">
          <div class="outcome-choice-label">Select outcome:</div>
          <div class="actions">${buttons}</div>
        </div>`;
      }
      // Show choice selector if step has choices
      if (isManual && detail?.choices) {
        const ch = detail.choices;
        const buttons = ch.options.map((o: any) => {
          return `<button class="btn btn-secondary" style="display:block;width:100%;text-align:left;margin:4px 0;padding:8px 12px" onclick="submitChoice('${escapeHtml(detail.stepId)}', '${escapeHtml(ch.variable)}', '${escapeHtml(o.value)}')">${escapeHtml(o.label || o.value)}${o.description ? `<div style='font-size:11px;opacity:0.6;margin-top:2px'>${escapeHtml(o.description)}</div>` : ''}</button>`;
        }).join('');
        return `<div class="choice-selector">
          <div style="font-weight:bold;margin-bottom:8px">${escapeHtml(ch.prompt || 'Select an option:')}</div>
          ${buttons}
        </div>`;
      }
      const btnLabel = isManual ? '✓ Mark Complete' : 'Next Step';
      return `<div class="actions">
      <button class="btn btn-primary" onclick="nextStep()"${disabledAttr}>${btnLabel}</button>
      ${!isManual && !busy ? '<button class="btn btn-secondary" onclick="getVars()">Variables</button>' : ''}
    </div>`;
    })() : ''}
  </div>
</div>
<script>
  const vscode = acquireVsCodeApi();
  function nextStep() { vscode.postMessage({ type: 'next' }); }
  function getVars() { vscode.postMessage({ type: 'getVariables' }); }
  function chooseOutcome(stepId, state) { vscode.postMessage({ type: 'chooseOutcome', stepId, state }); }
  function viewStep(stepId) { vscode.postMessage({ type: 'viewStep', stepId: stepId }); }
  function backStep() { vscode.postMessage({ type: 'backStep' }); }
  function returnToActive() { vscode.postMessage({ type: 'returnToActive' }); }
  function restart() { vscode.postMessage({ type: 'restart' }); }
  function copySummary() {
    const el = document.getElementById('summaryText');
    if (el) { vscode.postMessage({ type: 'copySummary', text: el.value }); }
  }
  function saveForReplay() {
    vscode.postMessage({ type: 'saveForReplay' });
  }
  function submitChoice(stepId, variable, value) {
    vscode.postMessage({ type: 'submitChoice', stepId: stepId, variable: variable, value: value });
  }
  function switchTab(tab) {
    vscode.postMessage({ type: 'switchTab', tab: tab });
  }
  function chainToRunbook() {
    vscode.postMessage({ type: 'chainToRunbook' });
  }
  function toggleBranch(el) {
    const connector = el.querySelector('.branch-connector');
    const content = el.nextElementSibling;
    if (content && content.classList.contains('branch-expanded-content')) {
      if (content.style.display === 'none') {
        content.style.display = 'block';
        connector.textContent = '▾';
        el.style.opacity = '0.7';
      } else {
        content.style.display = 'none';
        connector.textContent = '▸';
        el.style.opacity = '0.5';
      }
    }
  }
  function toggleMoreBranches(groupId) {
    const group = document.getElementById(groupId);
    const label = document.getElementById(groupId + '-label');
    if (group) {
      if (group.style.display === 'none') {
        group.style.display = 'block';
        if (label) label.textContent = label.textContent.replace('+', '−');
      } else {
        group.style.display = 'none';
        if (label) label.textContent = label.textContent.replace('−', '+');
      }
    }
  }
  function runAll() {
    vscode.postMessage({ type: 'runAll' });
  }
  function stopRunAll() {
    vscode.postMessage({ type: 'stopRunAll' });
  }
  function toggleInputs() {
    const body = document.getElementById('inputsBody');
    const arrow = document.getElementById('inputsArrow');
    if (body && arrow) {
      const open = body.style.display !== 'none';
      body.style.display = open ? 'none' : 'block';
      arrow.classList.toggle('open', !open);
    }
  }

  // Auto-scroll prose panel to active step
  (function() {
    const active = document.querySelector('.prose-content section.active');
    if (active) {
      // Scroll within the prose-panel container specifically
      const prosePanel = document.getElementById('prosePanel');
      if (prosePanel) {
        const rect = active.getBoundingClientRect();
        const panelRect = prosePanel.getBoundingClientRect();
        const scrollTop = prosePanel.scrollTop + (rect.top - panelRect.top) - (panelRect.height / 3);
        prosePanel.scrollTo({ top: Math.max(0, scrollTop), behavior: 'smooth' });
      }
    }
  })();

  // Draggable splitters
  (function() {
    // Left splitter resizes prose panel
    var leftDragging = false, leftStartX = 0, leftStartW = 0;
    var splitterLeft = document.getElementById('splitterLeft');
    var proseEl = document.getElementById('prosePanel');
    if (splitterLeft && proseEl) {
      splitterLeft.addEventListener('mousedown', function(e) {
        leftDragging = true; leftStartX = e.clientX; leftStartW = proseEl.offsetWidth;
        splitterLeft.classList.add('dragging');
        document.body.style.cursor = 'col-resize'; document.body.style.userSelect = 'none'; e.preventDefault();
      });
    }

    // Right splitter resizes workflow map
    var rightDragging = false, rightStartX = 0, rightStartW = 0;
    var splitterRight = document.getElementById('splitter');
    var mapEl = document.getElementById('workflowMap');
    if (splitterRight && mapEl) {
      splitterRight.addEventListener('mousedown', function(e) {
        rightDragging = true; rightStartX = e.clientX; rightStartW = mapEl.offsetWidth;
        splitterRight.classList.add('dragging');
        document.body.style.cursor = 'col-resize'; document.body.style.userSelect = 'none'; e.preventDefault();
      });
    }

    document.addEventListener('mousemove', function(e) {
      if (leftDragging && proseEl) {
        proseEl.style.flex = 'none';
        proseEl.style.width = Math.max(150, Math.min(leftStartW + (e.clientX - leftStartX), window.innerWidth - 500)) + 'px';
      }
      if (rightDragging && mapEl) {
        mapEl.style.width = Math.max(160, Math.min(rightStartW + (e.clientX - rightStartX), window.innerWidth - 400)) + 'px';
      }
    });
    document.addEventListener('mouseup', function() {
      if (leftDragging && proseEl) {
        leftDragging = false;
        if (splitterLeft) splitterLeft.classList.remove('dragging');
        vscode.postMessage({ type: 'saveSplitter', panel: 'prose', width: proseEl.style.width });
      }
      if (rightDragging && mapEl) {
        rightDragging = false;
        if (splitterRight) splitterRight.classList.remove('dragging');
        vscode.postMessage({ type: 'saveSplitter', panel: 'map', width: mapEl.style.width });
      }
      document.body.style.cursor = ''; document.body.style.userSelect = '';
    });
  })();
</script>
</body>
</html>`;
  }

  /**
   * Auto-advance a single step during Run All replay.
   * If the step is manual with outcome choices, auto-select the matching outcome.
   * Otherwise proceed normally with execNext.
   */
  private async autoAdvanceStep() {
    if (!this.autoRunning) return;
    const detail = this.currentStepDetail;
    if (!detail) return;

    try {
      const isManual = detail.type === 'manual';
      const hasOutcomes = detail.outcomes && detail.outcomes.length > 0;

      if (isManual && hasOutcomes) {
        // Derive expected outcome from scenario folder name
        const scenarioDir = this.creationArgs?.options?.scenarioDir || '';
        const folderName = scenarioDir.replace(/[\\/]+$/, '').split(/[\\/]/).pop() || '';
        // Strip auto-increment suffix (e.g., "resolved-2" → "resolved")
        const expected = folderName.replace(/-\d+$/, '');
        // Find matching outcome, fallback to first
        const match = detail.outcomes.find((o: any) => o.state === expected) || detail.outcomes[0];
        await this.client.chooseOutcome(detail.stepId, match.state);
      } else {
        await this.client.execNext();
      }
    } catch (e: any) {
      this.autoRunning = false;
      this.updateWebview();
      vscode.window.showErrorMessage(`Gert: Run All stopped — ${e.message}`);
    }
  }

  /**
   * Highlight the source TSG section corresponding to the active step.
   */
  private async highlightSourceStep(stepId: string) {
    if (!this.sourceFilePath) return;
    const range = this.sourceMapping[stepId];
    if (!range) return;

    try {
      const doc = await vscode.workspace.openTextDocument(this.sourceFilePath);
      // Find if already visible, otherwise show it
      let editor = vscode.window.visibleTextEditors.find(
        e => e.document.uri.fsPath === doc.uri.fsPath
      );
      if (!editor) {
        editor = await vscode.window.showTextDocument(doc, vscode.ViewColumn.One, true);
      }

      // Lines in mapping are 1-based, VS Code ranges are 0-based
      const startLine = range.start - 1;
      const endLine = range.end - 1;
      const highlightRange = new vscode.Range(startLine, 0, endLine, 0);

      // Apply highlight decoration
      editor.setDecorations(this.highlightDecoration, [highlightRange]);

      // Scroll to reveal the range
      editor.revealRange(highlightRange, vscode.TextEditorRevealType.InCenterIfOutsideViewport);
    } catch {
      // ignore — best effort
    }
  }

  /**
   * Build a structured text summary for sharing (email, Teams, etc.)
   */
  private buildSummaryText(): string {
    const pathMod = require('path');
    const includeQueries = vscode.workspace.getConfiguration('gert').get<boolean>('summaryIncludeQueries', true);
    const name = this.creationArgs?.runbookPath?.split(/[\\/]/).pop()?.replace(/\.runbook\.(yaml|yml)$/i, '') || 'unknown';
    const ts = new Date().toISOString();
    const outcome = this.outcomeResult;
    const state = outcome?.state || '';
    const badge = outcomeIsConclusion(state) ? 'Result' : 'Recommendation';
    const headline = outcomeHeadline(state);

    let lines: string[] = [];
    lines.push(`## TSG Execution Summary`);
    lines.push(``);

    // Chain path
    if (this.chainHistory.length > 0) {
      const chainPath = [...this.chainHistory.map(e => e.name), name].join(' → ');
      lines.push(`**Execution chain:** ${chainPath}`);
      lines.push(``);
    }

    lines.push(`| | |`);
    lines.push(`|---|---|`);
    lines.push(`| **Final Runbook** | ${name} |`);
    lines.push(`| **Kind** | ${this.runbookKind} |`);
    lines.push(`| **Completed** | ${ts} |`);
    lines.push(`| **${badge}** | ${headline} |`);
    lines.push(``);

    // ── Parent TSG summaries ─────────────────────────────────────────
    for (let ci = 0; ci < this.chainHistory.length; ci++) {
      const entry = this.chainHistory[ci];
      lines.push(`### TSG ${ci + 1}: ${entry.name}`);
      lines.push(``);

      // Parent inputs
      const inputKeys = Object.keys(entry.inputs).sort();
      if (inputKeys.length > 0) {
        lines.push(`**Inputs:** ${inputKeys.map(k => `${k}=${entry.inputs[k]}`).join(', ')}`);
        lines.push(``);
      }

      // Parent captures
      const parentCaps = Object.entries(entry.captures).filter(([, v]) => {
        const val = String(v || '').trim();
        return val !== '' && val !== '[]' && val !== '{}' && val !== 'null';
      });
      if (parentCaps.length > 0) {
        lines.push(`**Findings:**`);
        for (const [k, v] of parentCaps) {
          lines.push(`- ${k}: ${v}`);
        }
        lines.push(``);
      }

      // Parent steps — with per-step captures
      const executed = entry.steps.filter(s => s.state === 'passed' || s.state === 'failed');
      if (executed.length > 0) {
        lines.push(`**Steps (${entry.name}):**`);
        lines.push(``);
        for (let si = 0; si < executed.length; si++) {
          const s = executed[si];
          const icon = s.state === 'passed' ? '✓' : '✗';
          const detail = entry.stepDetails?.get(s.id);
          lines.push(`**${ci + 1}.${si + 1} ${icon} ${s.title}**`);
          // Show query if present
          if (includeQueries && detail?.query) {
            lines.push(`\`\`\`kql`);
            lines.push(detail.query.trim());
            lines.push(`\`\`\``);
          }
          // Show captures from this step
          if (detail?.captures) {
            const caps = Object.entries(detail.captures as Record<string, string>).filter(([, v]) => v && String(v).trim() !== '');
            if (caps.length > 0) {
              lines.push(`| Variable | Value |`);
              lines.push(`|---|---|`);
              for (const [k, v] of caps) {
                lines.push(`| ${k} | ${v} |`);
              }
            }
          }
          lines.push(``);
        }
      }

      // Parent outcome
      if (entry.outcome) {
        if (entry.outcome.isRouting) {
          lines.push(`**Outcome:** Continued to next TSG`);
        } else {
          lines.push(`**Outcome:** ${outcomeHeadline(entry.outcome.state)}`);
        }
        lines.push(``);
      }
    }

    // ── Current TSG ──────────────────────────────────────────────────
    const tsgNum = this.chainHistory.length + 1;
    if (this.chainHistory.length > 0) {
      lines.push(`### TSG ${tsgNum}: ${name}`);
    } else {
      lines.push(`### Steps`);
    }
    lines.push(``);

    // Inputs — skip if same as parent (avoid repetition)
    const vars = this.creationArgs?.vars || {};
    const varNames = Object.keys(vars).sort();
    if (varNames.length > 0) {
      lines.push(`**Inputs:** ${varNames.map(k => `${k}=${vars[k] || '_(empty)_'}`).join(', ')}`);
      lines.push(``);
    }

    // Captures
    const capEntries = Object.entries(this.captures).filter(([, v]) => {
      const val = String(v || '').trim();
      return val !== '' && val !== '[]' && val !== '{}' && val !== 'null';
    });
    if (capEntries.length > 0) {
      lines.push(`**Findings:**`);
      for (const [k, v] of capEntries) {
        const val = String(v || '').trim();
        if (val.includes('\n') || val.length > 200) {
          lines.push(`- **${k}**:`);
          lines.push(`  \`\`\``);
          lines.push(`  ${val.split('\n').join('\n  ')}`);
          lines.push(`  \`\`\``);
        } else {
          lines.push(`- **${k}**: ${val}`);
        }
      }
      lines.push(``);
    }

    // Steps — with per-step captures and queries
    lines.push(`**Steps (${name}):**`);
    lines.push(``);
    const allSteps = this.steps.length > 0 ? this.steps : this.collectTreeSteps(this.tree);
    let currentNum = 1;
    for (const step of allSteps) {
      const stepState = this.stepStates.get(step.id) || 'pending';
      if (stepState === 'skipped' || stepState === 'pending') continue;
      const icon = stepState === 'passed' ? '✓' : '✗';
      const num = `${tsgNum}.${currentNum++}`;
      const detail = this.stepDetails.get(step.id);

      lines.push(`**${num} ${icon} ${step.title || step.id}**`);

      // Show query if present
      if (includeQueries && detail?.query) {
        lines.push(`\`\`\`kql`);
        lines.push(detail.query.trim());
        lines.push(`\`\`\``);
      }

      // Show captures from this step
      if (detail?.captures) {
        const caps = Object.entries(detail.captures as Record<string, string>).filter(([, v]) => v && String(v).trim() !== '');
        if (caps.length > 0) {
          lines.push(`| Variable | Value |`);
          lines.push(`|---|---|`);
          for (const [k, v] of caps) {
            lines.push(`| ${k} | ${v} |`);
          }
        }
      }
      lines.push(``);
    }

    // Final recommendation
    if (outcome?.recommendation) {
      lines.push(`### ${badge}`);
      lines.push(``);
      lines.push(outcome.recommendation);
      lines.push(``);
    }

    lines.push(`---`);
    lines.push(`_Generated by gert runbook engine_`);

    return lines.join('\n');
  }

  /** Snapshot current execution into a ChainEntry. */
  /**
   * Generate a test.yaml file from the completed run's observed state.
   * Called after "Save for Replay" when the user clicks "Save as Test Case".
   */
  private generateTestYaml(scenarioDir: string) {
    const pathMod = require('path');
    const fsMod = require('fs');
    const YAML_MOD = require('yaml');

    const testSpec: Record<string, any> = {};

    // expected_outcome
    if (this.outcomeResult?.state) {
      testSpec.expected_outcome = this.outcomeResult.state;
    } else if (this.runCompleted) {
      testSpec.expected_outcome = 'completed';
    }

    // expected_chain (from chainHistory + current runbook)
    if (this.chainHistory.length > 0) {
      testSpec.expected_chain = this.chainHistory.map((e: ChainEntry) => e.name);
      const currentName = pathMod.basename(this.creationArgs?.runbookPath || '')
        .replace(/\.runbook\.(yaml|yml)$/i, '');
      if (currentName) {
        testSpec.expected_chain.push(currentName);
      }
    }

    // expected_captures (non-empty, non-placeholder values)
    const captures: Record<string, string> = {};
    for (const [k, v] of Object.entries(this.captures)) {
      if (v && v !== '<dry-run>' && v !== '<no value>') {
        captures[k] = v;
      }
    }
    if (Object.keys(captures).length > 0) {
      testSpec.expected_captures = captures;
    }

    // must_reach: all steps that were visited (passed or failed)
    const reached: string[] = [];
    for (const [id, state] of this.stepStates.entries()) {
      if (state === 'passed' || state === 'failed') {
        reached.push(id);
      }
    }
    if (reached.length > 0) {
      testSpec.must_reach = reached;
    }

    // expected_step_status
    const stepStatuses: Record<string, string> = {};
    for (const [id, state] of this.stepStates.entries()) {
      if (state === 'passed' || state === 'failed' || state === 'skipped') {
        stepStatuses[id] = state;
      }
    }
    if (Object.keys(stepStatuses).length > 0) {
      testSpec.expected_step_status = stepStatuses;
    }

    try {
      const testYaml = YAML_MOD.stringify(testSpec);
      fsMod.writeFileSync(pathMod.join(scenarioDir, 'test.yaml'), testYaml, 'utf-8');
      vscode.window.showInformationMessage(`Gert: Test case saved → ${pathMod.join(scenarioDir, 'test.yaml')}`);
    } catch (e: any) {
      vscode.window.showErrorMessage(`Gert: Failed to write test.yaml — ${e.message}`);
    }
  }

  private snapshotChainEntry(): ChainEntry {
    const pathMod = require('path');
    const name = pathMod.basename(this.creationArgs?.runbookPath || '').replace(/\.runbook\.(yaml|yml)$/i, '');
    const allSteps = this.steps.length > 0 ? this.steps : this.collectTreeSteps(this.tree);
    return {
      name,
      runbookPath: this.creationArgs?.runbookPath || '',
      inputs: { ...(this.creationArgs?.vars || {}) },
      captures: { ...this.captures },
      tree: JSON.parse(JSON.stringify(this.tree)), // deep copy
      stepStates: new Map(this.stepStates),
      stepDetails: new Map(this.stepDetails),
      prose: this.prose,
      description: this.runbookDescription,
      steps: allSteps.map((s: any) => {
        const detail = this.stepDetails.get(s.id);
        const title = detail?.title || s.title || s.id;
        return {
          id: s.id,
          title,
          state: this.stepStates.get(s.id) || 'pending',
        };
      }),
      outcome: this.outcomeResult ? {
        state: this.outcomeResult.state,
        recommendation: this.outcomeResult.recommendation || '',
        isRouting: !!this.pendingNextRunbook,
      } : null,
    };
  }

  /**
   * Collect all steps from a tree structure into a flat list.
   */
  // ══════════════════════════════════════════════════════════════════
  // TSG Prose Panel — rendered HTML view of the runbook
  // ══════════════════════════════════════════════════════════════════

  /** Create or update the TSG prose panel (left side). */
  /** Safely render prose HTML, catching any errors */
  private getProseHtml(): string {
    try {
      return this.renderRunbookAsHTML();
    } catch (e: any) {
      return `<div style="color:var(--vscode-errorForeground);padding:16px"><strong>Error rendering TSG prose:</strong><pre>${this.escapeForProse(e.message || String(e))}\n${this.escapeForProse(e.stack || '')}</pre></div>`;
    }
  }

  /** Render the runbook as a prose TSG document (like a human-written markdown TSG, rendered as HTML).
   *  The structure follows TSG conventions: Title, Background, Triage, Mitigation, Escalation.
   *  Each step becomes a subsection. Kusto queries become code blocks. Branch decisions become
   *  bullet lists. The active step is wrapped in a section tag for highlighting. */
  private renderRunbookAsHTML(): string {
    const pathMod = require('path');

    // Determine which TSG to show based on viewingStepId
    // If viewing a step from a chain history entry, show that entry's full prose
    let activeTsgIndex = this.chainHistory.length; // default: current TSG
    if (this.viewingStepId) {
      for (let ci = 0; ci < this.chainHistory.length; ci++) {
        if (this.chainHistory[ci].steps.some(s => s.id === this.viewingStepId)) {
          activeTsgIndex = ci;
          break;
        }
      }
    }

    // Choose the data source based on which TSG is active
    let name: string, prose: any, description: string, tree: any[], states: Map<string, string>, details: Map<string, any>;
    if (activeTsgIndex < this.chainHistory.length) {
      const entry = this.chainHistory[activeTsgIndex];
      name = entry.name;
      prose = entry.prose || {};
      description = entry.description || '';
      tree = entry.tree;
      states = entry.stepStates;
      details = entry.stepDetails;
    } else {
      name = pathMod.basename(this.creationArgs?.runbookPath || '').replace(/\.runbook\.(yaml|yml)$/i, '');
      prose = this.prose || {};
      description = this.runbookDescription;
      tree = this.tree;
      states = this.stepStates;
      details = this.stepDetails;
    }

    const title = this.titleCase(name.replace(/-/g, ' '));

    // TSG navigation tabs
    let html = `<div style="display:flex;gap:4px;margin-bottom:12px;flex-wrap:wrap">`;
    for (let ci = 0; ci < this.chainHistory.length; ci++) {
      const e = this.chainHistory[ci];
      const isActive = ci === activeTsgIndex;
      html += `<span style="padding:3px 8px;border-radius:3px;font-size:11px;cursor:default;${isActive ? 'background:var(--vscode-button-background);color:var(--vscode-button-foreground);font-weight:bold' : 'background:rgba(255,255,255,0.05);opacity:0.6'}">${ci + 1}. ${this.escapeForProse(e.name)}</span>`;
    }
    const isCurrent = activeTsgIndex === this.chainHistory.length;
    const currentName = pathMod.basename(this.creationArgs?.runbookPath || '').replace(/\.runbook\.(yaml|yml)$/i, '');
    html += `<span style="padding:3px 8px;border-radius:3px;font-size:11px;cursor:default;${isCurrent ? 'background:var(--vscode-button-background);color:var(--vscode-button-foreground);font-weight:bold' : 'background:rgba(255,255,255,0.05);opacity:0.6'}">${this.chainHistory.length + 1}. ${this.escapeForProse(currentName)}</span>`;
    html += `</div>\n`;

    // ── Title ─────────────────────────────
    html += `<h1>${this.escapeForProse(title)}</h1>\n`;

    // ── Safety warning ────────────────────
    if (prose.safety) {
      html += `<div style="padding:10px 14px;background:rgba(255,200,0,0.08);border-left:3px solid var(--vscode-charts-yellow);margin:12px 0;border-radius:4px">`;
      html += `<strong>⚠ Production Touch Safety</strong><br/>`;
      html += this.renderProseMarkdown(prose.safety);
      html += `</div>\n`;
    }

    // ── Background ────────────────────────
    const background = prose.background || description;
    if (background) {
      html += `<h2>Background</h2>\n`;
      html += this.renderProseMarkdown(background);
    }

    // ── Prerequisites ─────────────────────
    if (prose.prerequisites) {
      html += `<h2>Pre-requisites</h2>\n`;
      html += this.renderProseMarkdown(prose.prerequisites);
    }

    // ── Triage section ────────────────────
    html += `<h2>Triage</h2>\n`;
    const triageSteps: any[] = [];
    const mitigationSteps: any[] = [];
    this.classifyStepsForProse(tree, triageSteps, mitigationSteps);

    for (const { node, depth } of triageSteps) {
      html += this.renderStepAsProse(node, states, depth);
    }

    // ── Mitigation section ────────────────
    if (mitigationSteps.length > 0) {
      html += `<h2>Mitigation</h2>\n`;
      for (const { node, depth } of mitigationSteps) {
        html += this.renderStepAsProse(node, states, depth);
      }
    }

    // ── Post-Mitigation ───────────────────
    if (prose.post_mitigation) {
      html += `<h2>Post-Mitigation</h2>\n`;
      html += this.renderProseMarkdown(prose.post_mitigation);
    }

    // ── Escalation ────────────────────────
    const escalateOutcomes = this.collectEscalationOutcomes(tree);
    if (escalateOutcomes.length > 0) {
      html += `<h2>Escalation</h2>\n`;
      for (const o of escalateOutcomes) {
        html += this.renderProseMarkdown(this.resolveTemplateVars(o));
      }
    }

    // ── References ────────────────────────
    if (prose.references && prose.references.length > 0) {
      html += `<h2>References</h2>\n<ul>\n`;
      for (const ref of prose.references) {
        html += `<li><a href="${this.escapeForProse(ref.url || ref.URL || '')}">${this.escapeForProse(ref.title || ref.Title || '')}</a></li>\n`;
      }
      html += `</ul>\n`;
    }

    // ── Ownership ─────────────────────────
    const ownership = prose.ownership || prose.Ownership;
    if (ownership) {
      html += `<h2>Ownership</h2>\n`;
      html += `<p>Team: <a href="mailto:${this.escapeForProse(ownership.email || ownership.Email || '')}">${this.escapeForProse(ownership.team || ownership.Team || '')}</a></p>\n`;
    }

    return html;
  }

  /** Classify tree steps into triage (queries/checks) and mitigation (actions/routing). */
  private classifyStepsForProse(nodes: any[], triage: any[], mitigation: any[], depth = 3) {
    for (const node of nodes) {
      if (node.iterate) {
        if (node.iterate.steps) this.classifyStepsForProse(node.iterate.steps, triage, mitigation, depth);
        continue;
      }
      const step = node.step;
      // Tool/CLI queries and checks go to triage; manual routing/actions go to mitigation
      if (step.type === 'cli' || step.type === 'tool' || (step.type === 'manual' && !step.outcomes?.some((o: any) => o.next_runbook))) {
        triage.push({ node, depth });
      } else {
        mitigation.push({ node, depth });
      }
      // Include branches
      if (node.branches) {
        for (const branch of node.branches) {
          if (branch.steps) {
            this.classifyStepsForProse(branch.steps, triage, mitigation, depth);
          }
        }
      }
    }
  }

  /** Render a single step as prose paragraph/section. */
  private renderStepAsProse(node: any, states: Map<string, string>, headingLevel: number): string {
    const step = node.step;
    const state = states.get(step.id) || 'pending';
    const detail = this.stepDetails.get(step.id);
    const title = detail?.title || step.title || step.id;
    const instructions = detail?.instructions || step.instructions || '';
    const h = `h${Math.min(headingLevel, 6)}`;
    const isActive = (this.viewingStepId || this.currentStepDetail?.stepId) === step.id;

    let html = `<section data-step-id="${step.id}" class="${isActive ? 'active' : ''}">\n`;
    html += `<${h}>${this.escapeForProse(title)}</${h}>\n`;

    // Query as code block — check step.query (from server)
    const query = step.query;
    if (query) {
      const resolvedQuery = detail?.query || query;
      html += `<pre><code class="language-kusto">${this.highlightKusto(resolvedQuery)}</code></pre>\n`;
    }

    // Instructions as prose
    if (instructions) {
      html += this.renderProseMarkdown(this.resolveTemplateVars(instructions));
    }

    // Outcomes as prose callouts
    if (step.outcomes && step.outcomes.length > 0) {
      for (const o of step.outcomes) {
        if (o.recommendation) {
          const stateLabel = o.state === 'no_action' ? 'If no issues found' :
                            o.state === 'escalated' ? 'Request Assistance' :
                            o.state === 'resolved' ? 'Resolution' : o.state;
          const resolvedRec = this.resolveTemplateVars(o.recommendation);
          html += `<blockquote><strong>${this.escapeForProse(stateLabel)}:</strong> ${this.renderProseMarkdown(resolvedRec)}</blockquote>\n`;
        }
      }
    }

    html += `</section>\n`;
    return html;
  }

  /** Resolve {{ .var }} template variables using current captures and inputs. */
  private resolveTemplateVars(text: string): string {
    const vars: Record<string, string> = { ...(this.creationArgs?.vars || {}), ...this.captures };
    // Also include chain history captures
    for (const entry of this.chainHistory) {
      Object.assign(vars, entry.captures);
    }
    return text.replace(/\{\{\s*\.(\w+)\s*\}\}/g, (_match, varName) => {
      return vars[varName] || `{{ .${varName} }}`;
    });
  }

  /** Render tree recursively as flat prose sections (no tree indentation). */
  private renderTreeAsProse(nodes: any[], states: Map<string, string>, headingLevel: number): string {
    let html = '';
    for (const node of nodes) {
      if (node.iterate) {
        if (node.iterate.steps) {
          html += this.renderTreeAsProse(node.iterate.steps, states, headingLevel);
        }
        continue;
      }
      html += this.renderStepAsProse(node, states, headingLevel);
      if (node.branches) {
        for (const branch of node.branches) {
          if (branch.steps) {
            html += this.renderTreeAsProse(branch.steps, states, headingLevel);
          }
        }
      }
    }
    return html;
  }

  /** Collect escalation recommendation texts from outcome nodes. */
  private collectEscalationOutcomes(nodes: any[]): string[] {
    const results: string[] = [];
    for (const node of nodes) {
      if (node.iterate) {
        if (node.iterate.steps) results.push(...this.collectEscalationOutcomes(node.iterate.steps));
        continue;
      }
      if (node.step?.outcomes) {
        for (const o of node.step.outcomes) {
          if (o.state === 'escalated' && o.recommendation) {
            results.push(o.recommendation);
          }
        }
      }
      if (node.branches) {
        for (const branch of node.branches) {
          if (branch.steps) results.push(...this.collectEscalationOutcomes(branch.steps));
        }
      }
    }
    return results;
  }

  /** Render the source markdown file as a preformatted view. */
  private renderRunbookAsMarkdownView(): string {
    if (this.sourceFilePath) {
      try {
        const fs = require('fs');
        const content = fs.readFileSync(this.sourceFilePath, 'utf-8');
        return `<div class="markdown-source">${this.escapeForProse(content)}</div>`;
      } catch {
        return '<p>Source markdown not available.</p>';
      }
    }
    return '<p>No source file associated with this runbook.</p>';
  }

  /** Resolve image paths in prose markdown for the webview. */
  private renderProseMarkdown(text: string): string {
    let processed = text;
    // Resolve relative image paths using the main panel's webview
    if (this.sourceDirUri) {
      const pathMod = require('path');
      processed = processed.replace(/!\[([^\]]*)\]\(([^)]+)\)/g, (_match: string, alt: string, src: string) => {
        if (!src.startsWith('http://') && !src.startsWith('https://')) {
          const absPath = pathMod.resolve(this.sourceDirUri!.fsPath, src);
          const webviewUri = this.panel.webview.asWebviewUri(vscode.Uri.file(absPath)).toString();
          return `<img src="${webviewUri}" alt="${this.escapeForProse(alt)}" />`;
        }
        return `<img src="${src}" alt="${this.escapeForProse(alt)}" />`;
      });
    }
    // Simple markdown→HTML: bold, code, links, bullets
    processed = processed.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
    processed = processed.replace(/`([^`]+)`/g, '<code>$1</code>');
    processed = processed.replace(/\[([^\]]+)\]\(([^)]+)\)/g, '<a href="$2">$1</a>');
    processed = processed.replace(/^- (.+)$/gm, '<li>$1</li>');
    processed = processed.replace(/(?:<li>.*<\/li>\n?)+/g, (m) => `<ul>${m}</ul>`);
    processed = processed.replace(/\n\n/g, '</p><p>');
    return `<p>${processed}</p>`;
  }

  private escapeForProse(s: string): string {
    return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
  }

  /** Highlight Kusto/KQL code using highlight.js */
  private highlightKusto(code: string): string {
    try {
      return hljs.highlight(code, { language: 'kusto' }).value;
    } catch {
      return this.escapeForProse(code);
    }
  }

  private titleCase(s: string): string {
    return s.replace(/\b\w/g, c => c.toUpperCase());
  }

  private collectTreeSteps(nodes: any[]): any[] {
    const result: any[] = [];
    for (const node of nodes) {
      if (node.step) result.push(node.step);
      if (node.iterate && node.iterate.steps) {
        result.push(...this.collectTreeSteps(node.iterate.steps));
      }
      if (node.branches) {
        for (const branch of node.branches) {
          if (branch.steps) result.push(...this.collectTreeSteps(branch.steps));
        }
      }
    }
    return result;
  }

  private renderTree(nodes: any[], depth: number, stateOverride?: Map<string, string>, numberMap?: Map<string, string>, chainOutcome?: { state: string; isRouting?: boolean } | null): string {
    const states = stateOverride || this.stepStates;
    const outcomeCtx = chainOutcome !== undefined ? chainOutcome : this.outcomeResult;
    const runDone = !!outcomeCtx || this.runCompleted;
    const isReplay = this.creationArgs?.mode === 'replay';
    const freeNav = this.runbookKind !== 'mitigation' || runDone || isReplay;
    const showNumbers = vscode.workspace.getConfiguration('gert').get<boolean>('showStepNumbers', true);
    return nodes.map((node: any) => {
      // Handle iterate nodes — render as loop container with inner steps
      if (node.iterate) {
        const iter = node.iterate;
        const indent = depth * 20;
        const is = this.iterateState;
        let statusHtml = '';
        let icon = '🔄';
        let titleText = '';
        if (iter.mode === 'over' || iter.as) {
          // List mode: "Iterate over N items (as: varname)"
          const total = iter.total ?? 0;
          const asVar = iter.as || 'item';
          titleText = `Iterate over ${total} item${total !== 1 ? 's' : ''} (as: ${escapeHtml(asVar)})`;
          if (is) {
            if (is.status === 'converged') {
              icon = '✓';
              statusHtml = `<span style="font-size:10px;opacity:0.7;margin-left:6px;color:var(--vscode-charts-green)">completed ${is.max} items</span>`;
            } else if (is.status === 'failed') {
              icon = '✗';
              statusHtml = `<span style="font-size:10px;opacity:0.7;margin-left:6px;color:var(--vscode-errorForeground)">failed at item ${is.pass}/${is.max}</span>`;
            } else if (is.item) {
              statusHtml = `<span style="font-size:10px;opacity:0.7;margin-left:6px">[${is.pass}/${is.max}] ${escapeHtml(is.item)}</span>`;
            } else {
              statusHtml = `<span style="font-size:10px;opacity:0.7;margin-left:6px">[${is.pass}/${is.max}]</span>`;
            }
          }
        } else {
          // Convergence mode: "Iterate (max N×, until: condition)"
          titleText = `Iterate (max ${iter.max}×, until: ${escapeHtml(iter.until || '')})`;
          if (is) {
            if (is.status === 'converged') {
              icon = '✓';
              statusHtml = `<span style="font-size:10px;opacity:0.7;margin-left:6px;color:var(--vscode-charts-green)">converged at pass ${is.pass}/${is.max}</span>`;
            } else if (is.status === 'failed') {
              icon = '✗';
              statusHtml = `<span style="font-size:10px;opacity:0.7;margin-left:6px;color:var(--vscode-errorForeground)">failed after ${is.max} passes</span>`;
            } else {
              statusHtml = `<span style="font-size:10px;opacity:0.7;margin-left:6px">pass ${is.pass}/${is.max}</span>`;
            }
          }
        }
        const stateClass = is?.status === 'converged' ? 'passed' : is?.status === 'failed' ? 'failed' : 'running';
        let html = `<div class="step ${stateClass}" style="padding-left:${indent + 8}px">
          <span class="state-icon">${icon}</span>
          <span class="step-title">${titleText}</span>
          ${statusHtml}
        </div>`;
        if (iter.steps && iter.steps.length > 0) {
          html += this.renderTree(iter.steps, depth + 1, stateOverride, numberMap, chainOutcome);
        }
        return html;
      }

      const step = node.step;
      const state = states.get(step.id) || 'pending';
      const icon = stateIcon(state);
      const typeIcon = stepTypeIcon(step.type);
      const error = this.stepErrors.get(step.id);
      const indent = depth * 20;
      const hasOutcomes = step.outcomes && step.outcomes.length > 0;
      const hasBranches = node.branches && node.branches.length > 0;
      const clickable = freeNav ? ' clickable' : '';
      const onclick = freeNav ? ` onclick="viewStep('${step.id}')"` : '';
      const num = numberMap?.get(step.id);
      const numLabel = (num && showNumbers) ? `<span style="font-size:10px;opacity:0.5;margin-right:2px">${num}</span>` : '';

      let html = `<div class="step ${state}${clickable}" style="padding-left:${indent + 8}px" data-step-id="${step.id}"${onclick}>
        <span class="state-icon">${icon}</span>
        <span class="type-icon">${typeIcon}</span>
        ${numLabel}<span class="step-title">${step.title || step.id}</span>
      </div>`;

      if (error) {
        html += `<div class="step-error" style="padding-left:${indent + 44}px">⚠ ${escapeHtml(error)}</div>`;
      }

      // Render outcomes inline
      if (hasOutcomes) {
        const reachedState = outcomeCtx?.state;
        const isRouting = (outcomeCtx as any)?.isRouting;
        const stepDone = state === 'passed' || state === 'failed';
        for (const o of step.outcomes) {
          // Dim outcomes unless this specific outcome was reached
          const isMatch = reachedState && o.state === reachedState;
          const isDimmed = state === 'skipped' || !isMatch;
          // For routing outcomes, show "continued →" instead of the raw state
          const displayLabel = isMatch && isRouting ? 'continued →' : o.state;
          const displayClass = isMatch && isRouting ? 'skipped' : (isDimmed ? 'skipped' : o.state);
          html += `<div class="tree-outcome${isDimmed ? ' skipped' : ''}" style="padding-left:${indent + 28}px">
            <span class="outcome-arrow">└─</span>
            <span class="outcome-state-label ${displayClass}">${displayLabel}</span>
            ${o.when ? `<span class="outcome-when">${escapeHtml(o.when)}</span>` : '<span class="outcome-when">(default)</span>'}
          </div>`;
        }
      }

      // Render branches — taken branches expanded, skipped branches collapsed
      if (hasBranches) {
        // Classify branches: taken (has any non-skipped/non-pending step) vs skipped
        const takenBranches: any[] = [];
        const skippedBranches: any[] = [];
        for (const branch of node.branches) {
          const allSkippedOrPending = branch.steps?.every((n: any) => {
            if (!n.step) return true; // iterate nodes — treat as pending
            const s = states.get(n.step.id);
            return s === 'skipped' || s === 'pending' || !s;
          }) ?? true;
          if (allSkippedOrPending) {
            skippedBranches.push(branch);
          } else {
            takenBranches.push(branch);
          }
        }

        // Render taken branches fully expanded
        for (const branch of takenBranches) {
          html += `<div class="branch" style="padding-left:${indent + 8}px">
            <div class="branch-header">
              <span class="branch-connector" style="color:var(--vscode-charts-green)">▼</span>
              <span class="branch-label" style="font-weight:bold">${escapeHtml(branch.label || branch.condition)}</span>
            </div>
          </div>`;
          if (branch.steps && branch.steps.length > 0) {
            html += this.renderTree(branch.steps, depth + 1, stateOverride, numberMap, chainOutcome);
          }
        }

        // Render skipped branches as collapsed one-liners
        const MAX_VISIBLE_SKIPPED = 3;
        const visibleSkipped = skippedBranches.slice(0, MAX_VISIBLE_SKIPPED);
        const hiddenCount = skippedBranches.length - MAX_VISIBLE_SKIPPED;
        const groupId = `skipped-${step.id}-${depth}`;

        for (const branch of visibleSkipped) {
          html += `<div class="branch skipped collapsed-branch" style="padding-left:${indent + 8}px;cursor:pointer;opacity:0.5" onclick="toggleBranch(this)">
            <div class="branch-header">
              <span class="branch-connector">▸</span>
              <span class="branch-label">${escapeHtml(branch.label || branch.condition)}</span>
              <span style="font-size:10px;opacity:0.6;margin-left:4px">(skipped)</span>
            </div>
            <div class="branch-body" style="display:none">
          </div>`;
          if (branch.steps && branch.steps.length > 0) {
            html += `<div style="display:none" class="branch-expanded-content">`;
            html += this.renderTree(branch.steps, depth + 1, stateOverride, numberMap, chainOutcome);
            html += `</div>`;
          }
          html += `</div>`;
        }

        if (hiddenCount > 0) {
          html += `<div class="branch skipped" style="padding-left:${indent + 8}px;opacity:0.4;cursor:pointer" onclick="toggleMoreBranches('${groupId}')">
            <div class="branch-header">
              <span class="branch-connector">···</span>
              <span class="branch-label" id="${groupId}-label">+${hiddenCount} more skipped branches</span>
            </div>
          </div>`;
          html += `<div id="${groupId}" style="display:none">`;
          for (const branch of skippedBranches.slice(MAX_VISIBLE_SKIPPED)) {
            html += `<div class="branch skipped collapsed-branch" style="padding-left:${indent + 8}px;opacity:0.5;cursor:pointer" onclick="toggleBranch(this)">
              <div class="branch-header">
                <span class="branch-connector">▸</span>
                <span class="branch-label">${escapeHtml(branch.label || branch.condition)}</span>
                <span style="font-size:10px;opacity:0.6;margin-left:4px">(skipped)</span>
              </div>
            </div>`;
            if (branch.steps && branch.steps.length > 0) {
              html += `<div style="display:none" class="branch-expanded-content">`;
              html += this.renderTree(branch.steps, depth + 1, stateOverride, numberMap, chainOutcome);
              html += `</div>`;
            }
          }
          html += `</div>`;
        }
      }

      return html;
    }).join('');
  }

  private renderFlatSteps(): string {
    return this.steps.map((step, i) => {
      const state = this.stepStates.get(step.id) || 'pending';
      const icon = stateIcon(state);
      const typeIcon = stepTypeIcon(step.type);
      const error = this.stepErrors.get(step.id);
      return `<div class="step ${state}" data-step-id="${step.id}">
        <span class="state-icon">${icon}</span>
        <span class="type-icon">${typeIcon}</span>
        <span class="step-title">${step.title || step.id}</span>
        ${step.when ? '<span class="when-badge">conditional</span>' : ''}

      </div>
      ${error ? `<div class="step-error">⚠ ${escapeHtml(error)}</div>` : ''}`;
    }).join('');
  }

  private renderToolInfo(tool: any): string {
    if (!tool) return '';
    const gov = tool.governance;
    const govBadge = gov
      ? (gov.requires_approval
        ? '<span class="tool-gov-badge approval">⚠ requires approval</span>'
        : (gov.read_only
          ? '<span class="tool-gov-badge readonly">🔒 read-only</span>'
          : ''))
      : '';
    const argsHtml = tool.args && Object.keys(tool.args).length > 0
      ? `<div class="tool-args">
          <div class="tool-args-header">ARGS</div>
          ${Object.entries(tool.args).map(([k, v]: [string, any]) =>
            `<div class="tool-arg-row">
              <span class="tool-arg-name">${escapeHtml(k)}</span>
              <span class="tool-arg-value">${escapeHtml(String(v))}</span>
            </div>`
          ).join('')}
        </div>`
      : '';
    return `<div class="tool-info">
      <div class="tool-header">
        <span class="tool-label">TOOL</span>
        <span class="tool-name">${escapeHtml(tool.name)}</span>
        <span class="tool-dot">·</span>
        <span class="tool-action">${escapeHtml(tool.action)}</span>
        ${govBadge}
      </div>
      ${argsHtml}
    </div>`;
  }

  private renderOutcomes(outcomes: any[], stepState?: string): string {
    if (!outcomes || outcomes.length === 0) return '';
    // If step already passed (no outcome fired), hide outcomes — they're visible in the workflow map
    if (stepState === 'passed') return '';
    // Gate by display config
    if (!this.showOutcomeConditions()) return '';
    // For running/pending steps, show what outcomes are possible (without recommendation text)
    return `<div class="outcomes-section">
      <h3>Possible Outcomes</h3>
      ${outcomes.map((o: any) => `
        <div class="outcome-item">
          <span class="state-label ${o.state}">${o.state}</span>
          ${o.when ? `<span class="when-expr">${escapeHtml(o.when)}</span>` : '<span class="when-expr">(default)</span>'}
        </div>
      `).join('')}
    </div>`;
  }

  /** Find step detail by ID — searches current stepDetails, chain history, and tree nodes. */
  private findStepDetail(stepId: string): any {
    // 1. Current TSG stepDetails (from server events)
    const current = this.stepDetails.get(stepId);
    if (current) return current;

    // 2. Chain history stepDetails
    for (const entry of this.chainHistory) {
      const detail = entry.stepDetails.get(stepId);
      if (detail) return detail;
    }

    // 3. Current tree nodes
    const treeNode = this.findStepInTree(stepId, this.tree);
    if (treeNode) return treeNode;

    // 4. Chain history tree nodes
    for (const entry of this.chainHistory) {
      const node = this.findStepInTree(stepId, entry.tree);
      if (node) return node;
    }

    return null;
  }

  /** Find a step's data in the tree by ID (for client-side browsing). */
  private findStepInTree(stepId: string, nodes: any[]): any {
    for (const node of nodes) {
      if (node.step?.id === stepId) return node.step;
      if (node.branches) {
        for (const branch of node.branches) {
          if (branch.steps) {
            const found = this.findStepInTree(stepId, branch.steps);
            if (found) return found;
          }
        }
      }
    }
    return null;
  }

  /** Light markdown-ish rendering for instructions: links, bold, bullets, images */
  private renderMarkdownish(text: string): string {
    // Resolve relative image paths before Markdown rendering
    let processed = text.replace(/!\[([^\]]*)\]\(([^)]+)\)/g, (_match: string, alt: string, src: string) => {
      if (this.sourceDirUri && !src.startsWith('http://') && !src.startsWith('https://')) {
        const path = require('path');
        const absPath = path.resolve(this.sourceDirUri.fsPath, src);
        const webviewUri = this.panel.webview.asWebviewUri(vscode.Uri.file(absPath)).toString();
        return `![${alt}](${webviewUri})`;
      }
      return _match;
    });
    return marked.parse(processed, { async: false }) as string;
  }
}

function initTreeStates(nodes: any[], states: Map<string, string>) {
  for (const node of nodes) {
    if (node.step?.id) {
      states.set(node.step.id, 'pending');
    }
    if (node.iterate && node.iterate.steps) {
      initTreeStates(node.iterate.steps, states);
    }
    if (node.branches) {
      for (const branch of node.branches) {
        if (branch.steps) {
          initTreeStates(branch.steps, states);
        }
      }
    }
  }
}

/** Conclusions are facts; recommendations are advice. */
function outcomeIsConclusion(state: string): boolean {
  return state === 'resolved' || state === 'no_action';
}

/** Context-aware headline for outcome states. */
function outcomeHeadline(state: string): string {
  switch (state) {
    case 'resolved':   return 'RESOLVED';
    case 'no_action':  return 'NO ACTION NEEDED';
    case 'escalated':  return 'REQUEST ASSISTANCE';
    case 'needs_rca':  return 'RECOMMEND: NEEDS RCA';
    default:           return state.toUpperCase();
  }
}

/** Human-friendly button label for outcome states. */
function outcomeButtonLabel(state: string): string {
  switch (state) {
    case 'resolved':   return 'RESOLVED';
    case 'escalated':  return 'REQUEST ASSISTANCE';
    case 'no_action':  return 'NO ACTION';
    case 'needs_rca':  return 'NEEDS RCA';
    default:           return state.toUpperCase();
  }
}

function stateIcon(state: string): string {
  switch (state) {
    case 'passed': return '✓';
    case 'failed': return '✗';
    case 'skipped': return '⊘';
    case 'running': return '●';
    default: return '○';
  }
}

function stepTypeIcon(type: string): string {
  switch (type) {
    case 'cli': return '⚡';
    case 'manual': return '👤';
    case 'tool': return '🔧';
    case 'invoke': return '📦';
    default: return '•';
  }
}

function escapeHtml(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}

function formatCaptureValue(v: string): string {
  v = v.trim();
  // JSON array — show count + expandable content
  if (v.startsWith('[')) {
    try {
      const arr = JSON.parse(v);
      if (Array.isArray(arr)) {
        if (arr.length === 0) return '<span class="capture-count">0 rows</span>';
        const summary = `<span class="capture-count">${arr.length} row${arr.length > 1 ? 's' : ''}</span>`;
        const detail = escapeHtml(JSON.stringify(arr, null, 2));
        return `${summary} <pre class="capture-json">${detail}</pre>`;
      }
    } catch { /* not valid JSON */ }
  }
  // JSON object — format nicely
  if (v.startsWith('{')) {
    try {
      const obj = JSON.parse(v);
      const detail = escapeHtml(JSON.stringify(obj, null, 2));
      return `<pre class="capture-json">${detail}</pre>`;
    } catch { /* not valid JSON */ }
  }
  return escapeHtml(v);
}

function highlightQuery(query: string, queryType?: string): string {
  try {
    const result = hljs.highlight(query, { language: 'sql' });
    return result.value;
  } catch {
    return escapeHtml(query);
  }
}
