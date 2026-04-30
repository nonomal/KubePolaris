import { request } from '../utils/api';
import { buildWebSocketUrl } from '../utils/wsUrl';
import type { ArthasPlan, ArthasSessionResponse, ArthasStatus, ArthasExecResult, ArthasCommandDecision } from '../types/arthas';

const podArthasPath = (clusterId: string, namespace: string, name: string) =>
  `/clusters/${clusterId}/pods/${namespace}/${name}/arthas`;

export const arthasService = {
  getStatus(clusterId: string, namespace: string, name: string, container?: string): Promise<ArthasStatus> {
    const params = new URLSearchParams();
    if (container) {
      params.set('container', container);
    }
    const query = params.toString();
    const suffix = query ? `?${query}` : '';
    return request.get<ArthasStatus>(`${podArthasPath(clusterId, namespace, name)}/status${suffix}`);
  },

  createSession(
    clusterId: string,
    namespace: string,
    name: string,
    data: { container?: string; pid?: string }
  ): Promise<ArthasSessionResponse> {
    return request.post<ArthasSessionResponse>(`${podArthasPath(clusterId, namespace, name)}/sessions`, data);
  },

  buildPlan(
    clusterId: string,
    namespace: string,
    name: string,
    data: { prompt: string; evidence?: string[] }
  ): Promise<ArthasPlan> {
    return request.post<ArthasPlan>(`${podArthasPath(clusterId, namespace, name)}/agent/plan`, data);
  },

  confirmCommand(
    clusterId: string,
    namespace: string,
    name: string,
    commandId: string,
    data: { container: string; pid: string; command: string }
  ): Promise<{ result: ArthasExecResult; decision: ArthasCommandDecision }> {
    return request.post(`${podArthasPath(clusterId, namespace, name)}/commands/${commandId}/confirm`, data);
  },

  buildWebSocketUrl(clusterId: string, namespace: string, name: string, token: string, container?: string): string {
    const params = new URLSearchParams({ token });
    if (container) {
      params.set('container', container);
    }
    return buildWebSocketUrl(`/ws/clusters/${clusterId}/pods/${namespace}/${name}/arthas?${params}`);
  },
};
