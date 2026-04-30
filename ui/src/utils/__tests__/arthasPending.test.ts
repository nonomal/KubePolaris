import { describe, expect, it } from 'vitest';
import { isSamePendingCommand } from '../arthasPending';
import type { ArthasPlannedCommand } from '../../types/arthas';

const command = (id: string, value: string): ArthasPlannedCommand => ({
  id,
  command: value,
  purpose: value,
  risk: 'medium',
  requiresConfirmation: true,
});

describe('isSamePendingCommand', () => {
  it('does not treat different commands with empty ids as duplicates', () => {
    expect(isSamePendingCommand(command('', 'gc'), command('', 'vmoption'))).toBe(false);
  });

  it('deduplicates commands with matching non-empty ids', () => {
    expect(isSamePendingCommand(command('cmd-1', 'gc'), command('cmd-1', 'vmoption'))).toBe(true);
  });

  it('deduplicates commands with matching command text when ids are missing', () => {
    expect(isSamePendingCommand(command('', 'gc'), command('', 'gc'))).toBe(true);
  });
});
