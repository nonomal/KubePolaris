import type { ArthasPlannedCommand } from '../types/arthas';

export const isSamePendingCommand = (left: ArthasPlannedCommand, right: ArthasPlannedCommand) => {
  if (left.id && right.id) {
    return left.id === right.id;
  }
  return left.command === right.command;
};
