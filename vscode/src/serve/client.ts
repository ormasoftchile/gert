import * as vscode from 'vscode';
import * as cp from 'child_process';
import * as readline from 'readline';

/**
 * JSON-RPC message types matching pkg/serve/serve.go.
 */
export interface RPCMessage {
  jsonrpc: string;
  id?: number;
  method: string;
  params?: any;
  result?: any;
  error?: { code: number; message: string };
}

export interface StepSummary {
  id: string;
  type: string;
  title: string;
  index: number;
  when?: string;
  hasOutcomes?: boolean;
}

export interface StartResult {
  runId: string;
  baseDir: string;
  stepCount: number;
  steps: StepSummary[];
}

export interface StepEvent {
  stepId: string;
  index?: number;
  type?: string;
  title?: string;
  status?: string;
  captures?: Record<string, string>;
  error?: string;
  reason?: string;
}

export interface VariablesResult {
  vars: Record<string, string>;
  captures: Record<string, string>;
}

/**
 * GertClient manages a gert serve process over stdio JSON-RPC.
 */
export class GertClient {
  private process: cp.ChildProcess | null = null;
  private rl: readline.Interface | null = null;
  private nextId = 1;
  private pendingRequests = new Map<number, { resolve: (r: any) => void; reject: (e: Error) => void }>();
  private eventEmitter = new vscode.EventEmitter<RPCMessage>();

  /** Fires when gert sends an event (stepStarted, stepCompleted, etc.) */
  public readonly onEvent = this.eventEmitter.event;

  constructor(private gertPath: string) {}

  /** Get the path to the gert binary. */
  getGertPath(): string { return this.gertPath; }

  /**
   * Spawn gert serve and set up communication.
   */
  async start(): Promise<void> {
    console.log('gert: spawning', this.gertPath, 'serve');
    
    // Find the workspace folder that contains the gert binary for .env loading
    const gertDir = require('path').dirname(this.gertPath);
    const gertRoot = require('path').dirname(gertDir); // bin/gert.exe → parent dir
    
    this.process = cp.spawn(this.gertPath, ['serve'], {
      stdio: ['pipe', 'pipe', 'pipe'],
      cwd: gertRoot, // .env is in the gert project root
    });

    if (!this.process.stdout || !this.process.stdin) {
      throw new Error('Failed to spawn gert serve — no stdio');
    }

    this.process.on('error', (err) => {
      console.error('gert: spawn error:', err.message);
      vscode.window.showErrorMessage(`Gert: Failed to start gert serve — ${err.message}`);
    });

    this.rl = readline.createInterface({ input: this.process.stdout });

    this.rl.on('line', (line: string) => {
      if (!line.trim()) return;
      try {
        const msg: RPCMessage = JSON.parse(line);
        if (msg.id !== undefined && msg.id !== null && this.pendingRequests.has(msg.id)) {
          // Response to a request
          const pending = this.pendingRequests.get(msg.id)!;
          this.pendingRequests.delete(msg.id);
          if (msg.error) {
            pending.reject(new Error(msg.error.message));
          } else {
            pending.resolve(msg.result);
          }
        } else if (msg.method) {
          // Event notification
          this.eventEmitter.fire(msg);
        }
      } catch (e) {
        console.error('gert: failed to parse message:', line, e);
      }
    });

    this.process.stderr?.on('data', (data: Buffer) => {
      console.log('gert stderr:', data.toString());
    });

    this.process.on('exit', (code) => {
      console.log('gert serve exited with code', code);
      // Reject all pending requests
      for (const [, pending] of this.pendingRequests) {
        pending.reject(new Error('gert serve process exited'));
      }
      this.pendingRequests.clear();
    });
  }

  /**
   * Send a JSON-RPC request and wait for response.
   */
  private request<T>(method: string, params?: any): Promise<T> {
    return new Promise((resolve, reject) => {
      if (!this.process?.stdin) {
        reject(new Error('gert serve not running'));
        return;
      }
      const id = this.nextId++;
      this.pendingRequests.set(id, { resolve, reject });
      const msg: RPCMessage = { jsonrpc: '2.0', id, method, params: params || {} };
      this.process.stdin.write(JSON.stringify(msg) + '\n');
    });
  }

  /**
   * Start runbook execution.
   */
  async execStart(params: {
    runbook: string;
    mode: string;
    vars?: Record<string, string>;
    icmId?: string;
    scenarioDir?: string;
    rebaseTime?: string;
    actor?: string;
  }): Promise<StartResult> {
    return this.request<StartResult>('exec/start', params);
  }

  /**
   * Advance to next step.
   */
  async execNext(): Promise<any> {
    return this.request('exec/next');
  }

  /**
   * Choose an outcome for a manual step.
   */
  async chooseOutcome(stepId: string, state: string): Promise<any> {
    return this.request('exec/chooseOutcome', { stepId, state });
  }

  /**
   * Submit evidence for a manual step.
   */
  async submitChoice(stepId: string, variable: string, value: string): Promise<any> {
    return this.request('exec/submitChoice', { stepId, variable, value });
  }

  async submitEvidence(stepId: string, evidence: Record<string, any>): Promise<any> {
    return this.request('exec/submitEvidence', { stepId, evidence });
  }

  /**
   * Get current variables and captures.
   */
  async getVariables(): Promise<VariablesResult> {
    return this.request<VariablesResult>('exec/getVariables');
  }

  /**
   * Get run manifest.
   */
  async getManifest(): Promise<any> {
    return this.request('exec/getManifest');
  }

  /**
   * Save current run as a replay scenario folder.
   */
  async saveScenario(outputDir: string): Promise<{ status: string; outputDir: string }> {
    return this.request('exec/saveScenario', { outputDir });
  }

  /**
   * Shutdown the server.
   */
  async shutdown(): Promise<void> {
    try {
      await this.request('shutdown');
    } catch {
      // ignore — process may have already exited
    }
    this.dispose();
  }

  /**
   * Clean up resources.
   */
  dispose(): void {
    this.rl?.close();
    this.process?.kill();
    this.process = null;
    this.eventEmitter.dispose();
  }
}
