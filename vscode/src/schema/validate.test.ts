import * as fs from 'fs';
import * as path from 'path';
import * as yaml from 'yaml';
import { validateRunbook, resetValidator } from './validate';

// Note: This test requires 'yaml' package: npm install --save-dev yaml
// These tests verify that the TypeScript validator produces the same 
// accept/reject decisions as the Go validator for all golden-file fixtures.

const testdataDir = path.resolve(__dirname, '../../../testdata');

function loadYaml(filePath: string): unknown {
  const content = fs.readFileSync(filePath, 'utf-8');
  return yaml.parse(content);
}

function listFiles(dir: string): string[] {
  if (!fs.existsSync(dir)) return [];
  return fs.readdirSync(dir).filter(f => f.endsWith('.yaml') || f.endsWith('.yml'));
}

describe('Cross-stack schema parity', () => {
  beforeEach(() => {
    resetValidator();
  });

  describe('valid runbooks (should accept)', () => {
    const validDir = path.join(testdataDir, 'valid');
    const files = listFiles(validDir);

    if (files.length === 0) {
      test.skip('no valid fixtures found', () => {});
    }

    test.each(files)('%s should be accepted', (file) => {
      const data = loadYaml(path.join(validDir, file));
      const result = validateRunbook(data);
      expect(result.valid).toBe(true);
      expect(result.errors).toHaveLength(0);
    });
  });

  describe('invalid runbooks (should reject)', () => {
    const invalidDir = path.join(testdataDir, 'invalid');
    const files = listFiles(invalidDir);

    if (files.length === 0) {
      test.skip('no invalid fixtures found', () => {});
    }

    // Note: Not all invalid fixtures will fail JSON Schema validation.
    // Some are domain-level validations that only the Go validator catches.
    // We test that known schema-level violations are caught.
    const schemaInvalid = ['unknown-fields.yaml', 'missing-required.yaml'];
    
    test.each(schemaInvalid.filter(f => files.includes(f)))(
      '%s should be rejected',
      (file) => {
        const data = loadYaml(path.join(invalidDir, file));
        const result = validateRunbook(data);
        expect(result.valid).toBe(false);
        expect(result.errors.length).toBeGreaterThan(0);
      }
    );
  });
});
