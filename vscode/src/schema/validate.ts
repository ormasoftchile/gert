import Ajv2020, { ErrorObject } from 'ajv/dist/2020';
import addFormats from 'ajv-formats';
import * as fs from 'fs';
import * as path from 'path';

export interface ValidationError {
  phase: string;
  path: string;
  message: string;
}

export interface ValidationResult {
  valid: boolean;
  errors: ValidationError[];
}

let ajvInstance: Ajv2020 | null = null;
let compiledValidator: ReturnType<Ajv2020['compile']> | null = null;

/**
 * Loads and compiles the JSON Schema for runbook validation.
 */
function getValidator(): ReturnType<Ajv2020['compile']> {
  if (compiledValidator) {
    return compiledValidator;
  }

  // Try multiple paths: relative to dist/, relative to extension root, absolute
  const candidates = [
    path.resolve(__dirname, '..', 'schemas', 'runbook-v0.json'),   // dist/../schemas/
    path.resolve(__dirname, '../../schemas/runbook-v0.json'),       // dist/../../schemas/ (repo root)
    path.resolve(__dirname, '../schemas/runbook-v0.json'),          // alternate
  ];

  let schemaContent: string | null = null;
  for (const candidate of candidates) {
    try {
      schemaContent = fs.readFileSync(candidate, 'utf-8');
      break;
    } catch {
      // try next
    }
  }

  if (!schemaContent) {
    throw new Error(`Could not find runbook-v0.json schema. Searched: ${candidates.join(', ')}`);
  }

  const schema = JSON.parse(schemaContent);

  ajvInstance = new Ajv2020({
    allErrors: true,
    strict: false,
    validateFormats: true,
  });
  addFormats(ajvInstance);

  compiledValidator = ajvInstance.compile(schema);
  return compiledValidator;
}

/**
 * Validates a parsed YAML/JSON runbook object against the schema.
 */
export function validateRunbook(data: unknown): ValidationResult {
  const validate = getValidator();
  const valid = validate(data);

  if (valid) {
    return { valid: true, errors: [] };
  }

  const errors: ValidationError[] = (validate.errors || []).map(
    (err: ErrorObject) => ({
      phase: 'semantic',
      path: err.instancePath || '/',
      message: err.message || 'Unknown error',
    })
  );

  return { valid: false, errors };
}

/**
 * Resets cached validator (for testing).
 */
export function resetValidator(): void {
  ajvInstance = null;
  compiledValidator = null;
}
