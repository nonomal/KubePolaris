import { describe, it, expect, vi, beforeEach } from 'vitest';
import { arthasService } from '../arthasService';
import { request } from '../../utils/api';
import { buildWebSocketUrl } from '../../utils/wsUrl';

vi.mock('../../utils/api', () => ({
  request: {
    get: vi.fn(),
    post: vi.fn(),
  },
}));

vi.mock('../../utils/wsUrl', () => ({
  buildWebSocketUrl: vi.fn((path: string) => `ws://localhost${path}`),
}));

describe('arthasService', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('fetches Arthas status with optional container', async () => {
    vi.mocked(request.get).mockResolvedValue({ enabled: true });

    await arthasService.getStatus('1', 'default', 'app-pod', 'app');

    expect(request.get).toHaveBeenCalledWith(
      '/clusters/1/pods/default/app-pod/arthas/status?container=app'
    );
  });

  it('builds Arthas websocket url with token and container', () => {
    const url = arthasService.buildWebSocketUrl('1', 'default', 'app-pod', 'token', 'app');

    expect(buildWebSocketUrl).toHaveBeenCalledWith(
      '/ws/clusters/1/pods/default/app-pod/arthas?token=token&container=app'
    );
    expect(url).toBe('ws://localhost/ws/clusters/1/pods/default/app-pod/arthas?token=token&container=app');
  });
});
