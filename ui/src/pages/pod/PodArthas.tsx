import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import {
  Alert,
  Button,
  Card,
  Col,
  Empty,
  Input,
  List,
  Modal,
  Row,
  Select,
  Space,
  Spin,
  Tag,
  Typography,
  message,
} from 'antd';
import { ArrowLeftOutlined, BugOutlined, PlayCircleOutlined, ReloadOutlined } from '@ant-design/icons';
import { arthasService } from '../../services/arthasService';
import { PodService } from '../../services/podService';
import { parseApiError } from '../../utils/api';
import { isSamePendingCommand } from '../../utils/arthasPending';
import { tokenManager } from '../../services/authService';
import type { ArthasDiagnosisReport, ArthasExecResult, ArthasPlan, ArthasPlannedCommand, ArthasStatus, ArthasWSEvent } from '../../types/arthas';
import type { PodInfo } from '../../services/podService';

const { Text, Title, Paragraph } = Typography;
const { TextArea } = Input;

interface TimelineItem {
  id: string;
  type: string;
  title: string;
  content?: string;
  command?: string;
  risk?: string;
}

const isPlan = (value: unknown): value is ArthasPlan => {
  return Boolean(value && typeof value === 'object' && 'commands' in value);
};

const isPlannedCommand = (value: unknown): value is ArthasPlannedCommand => {
  return Boolean(value && typeof value === 'object' && 'command' in value && 'purpose' in value);
};

const isExecResult = (value: unknown): value is ArthasExecResult => {
  return Boolean(value && typeof value === 'object' && 'stdout' in value);
};

const isDiagnosisReport = (value: unknown): value is ArthasDiagnosisReport => {
  return Boolean(value && typeof value === 'object' && 'conclusion' in value);
};

const riskColor = (risk?: string) => {
  switch (risk) {
    case 'low':
      return 'green';
    case 'medium':
      return 'orange';
    case 'high':
      return 'red';
    default:
      return 'default';
  }
};

const PodArthas: React.FC = () => {
  const { clusterId, namespace, name } = useParams<{ clusterId: string; namespace: string; name: string }>();
  const navigate = useNavigate();
  const { t } = useTranslation('arthas');
  const { t: tc } = useTranslation('common');

  const wsRef = useRef<WebSocket | null>(null);
  const timelineEndRef = useRef<HTMLDivElement | null>(null);
  const [pod, setPod] = useState<PodInfo | null>(null);
  const [status, setStatus] = useState<ArthasStatus | null>(null);
  const [container, setContainer] = useState<string>();
  const [pid, setPid] = useState<string>();
  const [prompt, setPrompt] = useState('');
  const [timeline, setTimeline] = useState<TimelineItem[]>([]);
  const [pendingCommands, setPendingCommands] = useState<ArthasPlannedCommand[]>([]);
  const [loading, setLoading] = useState(false);
  const [diagnosing, setDiagnosing] = useState(false);
  const [connected, setConnected] = useState(false);

  const canAsk = Boolean(clusterId && namespace && name && container && pid && connected && prompt.trim() && !diagnosing && pendingCommands.length === 0);

  const evidence = useMemo(() => {
    return timeline
      .filter(item => item.type === 'result' && item.content)
      .map(item => `${item.command || ''}\n${item.content}`);
  }, [timeline]);

  const appendTimeline = useCallback((item: Omit<TimelineItem, 'id'>) => {
    setTimeline(prev => [...prev, { ...item, id: `${Date.now()}-${prev.length}` }]);
  }, []);

  const loadPodAndStatus = useCallback(async () => {
    if (!clusterId || !namespace || !name) return;
    setLoading(true);
    try {
      const podDetail = await PodService.getPodDetail(clusterId, namespace, name);
      setPod(podDetail.pod);
      const defaultContainer = container || podDetail.pod.containers[0]?.name;
      setContainer(defaultContainer);
      const nextStatus = await arthasService.getStatus(clusterId, namespace, name, defaultContainer);
      setStatus(nextStatus);
      if (!pid && nextStatus.processes.length > 0) {
        setPid(nextStatus.processes[0].pid);
      }
    } catch (error) {
      message.error(parseApiError(error));
    } finally {
      setLoading(false);
    }
  }, [clusterId, namespace, name, container, pid]);

  const connectWebSocket = useCallback(() => {
    if (!clusterId || !namespace || !name || !container) return;
    const token = tokenManager.getToken();
    if (!token) {
      message.error(t('messages.noToken'));
      return;
    }
    wsRef.current?.close();
    const ws = new WebSocket(arthasService.buildWebSocketUrl(clusterId, namespace, name, token, container));
    wsRef.current = ws;
    ws.onopen = () => setConnected(true);
    ws.onclose = () => setConnected(false);
    ws.onerror = () => message.error(t('messages.wsError'));
    ws.onmessage = event => {
      const payload = JSON.parse(event.data) as ArthasWSEvent;
      if (payload.type === 'connected') {
        appendTimeline({ type: 'info', title: t('timeline.connected'), content: payload.message });
      }
      if (payload.type === 'plan' && isPlan(payload.data)) {
        appendTimeline({
          type: 'plan',
          title: payload.data.intent,
          content: `${payload.data.reasoning}\n${payload.data.expectedSignals.join('\n')}`,
        });
      }
      if (payload.type === 'confirmation_required') {
        setDiagnosing(false);
      }
      if (payload.type === 'confirmation_required' && isPlannedCommand(payload.data)) {
        const command = payload.data;
        setPendingCommands(prev => {
          if (prev.some(item => isSamePendingCommand(item, command))) {
            return prev;
          }
          return [...prev, command];
        });
        appendTimeline({
          type: 'confirm',
          title: t('timeline.confirmRequired'),
          content: command.purpose,
          command: command.command,
          risk: command.risk,
        });
      }
      if (payload.type === 'command_result' && isExecResult(payload.data)) {
        appendTimeline({
          type: 'result',
          title: t('timeline.commandResult'),
          content: [payload.data.stdout, payload.data.stderr].filter(Boolean).join('\n'),
          command: payload.command,
          risk: payload.decision?.risk,
        });
      }
      if (payload.type === 'diagnosis_report' && isDiagnosisReport(payload.data)) {
        appendTimeline({
          type: 'report',
          title: t('timeline.report'),
          content: [
            payload.data.conclusion,
            '',
            t('timeline.possibleCauses'),
            ...payload.data.possibleCauses.map(item => `- ${item}`),
            '',
            t('timeline.recommendations'),
            ...payload.data.recommendations.map(item => `- ${item}`),
          ].join('\n'),
        });
      }
      if (payload.type === 'done') {
        setDiagnosing(false);
        appendTimeline({ type: 'done', title: t('timeline.done'), content: payload.message });
      }
      if (payload.type === 'error') {
        setDiagnosing(false);
        appendTimeline({ type: 'error', title: t('timeline.error'), content: payload.message, command: payload.command });
      }
    };
  }, [appendTimeline, clusterId, namespace, name, container, t]);

  useEffect(() => {
    loadPodAndStatus();
  }, [loadPodAndStatus]);

  useEffect(() => {
    if (container) {
      connectWebSocket();
    }
    return () => wsRef.current?.close();
  }, [container, connectWebSocket]);

  useEffect(() => {
    timelineEndRef.current?.scrollIntoView({ behavior: 'smooth', block: 'end' });
  }, [timeline.length]);

  const sendPrompt = () => {
    if (!canAsk || !wsRef.current) return;
    appendTimeline({ type: 'user', title: t('timeline.userPrompt'), content: prompt });
    setDiagnosing(true);
    wsRef.current.send(JSON.stringify({ type: 'prompt', prompt, pid, container, evidence }));
    setPrompt('');
  };

  const confirmPendingCommand = () => {
    const pendingCommand = pendingCommands[0];
    if (!pendingCommand || !wsRef.current) return;
    setDiagnosing(true);
    wsRef.current.send(JSON.stringify({
      type: 'confirm',
      commandId: pendingCommand.id,
      command: pendingCommand.command,
      pid,
      container,
    }));
    setPendingCommands(prev => prev.slice(1));
  };

  const cancelPendingCommand = () => {
    const pendingCommand = pendingCommands[0];
    if (pendingCommand && wsRef.current) {
      wsRef.current.send(JSON.stringify({
        type: 'skip_confirm',
        commandId: pendingCommand.id,
        command: pendingCommand.command,
      }));
    }
    setPendingCommands(prev => prev.slice(1));
  };

  const currentPendingCommand = pendingCommands[0];

  return (
    <div style={{ padding: 24 }}>
      <Space style={{ marginBottom: 16 }}>
        <Button icon={<ArrowLeftOutlined />} onClick={() => navigate(`/clusters/${clusterId}/pods/${namespace}/${name}`)}>
          {tc('actions.back')}
        </Button>
        <Title level={3} style={{ margin: 0 }}>
          <BugOutlined /> {t('title')}
        </Title>
        <Tag color={connected ? 'green' : 'red'}>{connected ? t('status.connected') : t('status.disconnected')}</Tag>
      </Space>

      <Spin spinning={loading}>
        <Row gutter={16}>
          <Col span={7}>
            <Card title={t('target.title')} extra={<Button icon={<ReloadOutlined />} onClick={loadPodAndStatus} />}>
              <Space direction="vertical" style={{ width: '100%' }}>
                <Text strong>{pod?.name}</Text>
                <Text type="secondary">{namespace}</Text>
                <Select
                  style={{ width: '100%' }}
                  value={container}
                  onChange={value => {
                    setContainer(value);
                    setPid(undefined);
                  }}
                  options={(pod?.containers || []).map(item => ({ label: item.name, value: item.name }))}
                />
                <Select
                  style={{ width: '100%' }}
                  value={pid}
                  placeholder={t('target.pidPlaceholder')}
                  onChange={setPid}
                  options={(status?.processes || []).map(item => ({
                    label: `${item.pid} ${item.mainClass}`,
                    value: item.pid,
                  }))}
                />
                <Alert
                  type={status?.arthasAvailable ? 'success' : 'warning'}
                  showIcon
                  message={status?.message || t('target.noStatus')}
                  description={status?.launcher ? `${t('target.launcher')}: ${status.launcher}` : undefined}
                />
              </Space>
            </Card>

            <Card title={t('ask.title')} style={{ marginTop: 16 }}>
              <Space direction="vertical" style={{ width: '100%' }}>
                <TextArea
                  rows={5}
                  value={prompt}
                  onChange={event => setPrompt(event.target.value)}
                  placeholder={t('ask.placeholder')}
                />
                <Button type="primary" icon={<PlayCircleOutlined />} block disabled={!canAsk} loading={diagnosing} onClick={sendPrompt}>
                  {t('ask.submit')}
                </Button>
              </Space>
            </Card>
          </Col>

          <Col span={17}>
            <Card title={t('timeline.title')}>
              {timeline.length === 0 ? (
                <Empty description={t('timeline.empty')} />
              ) : (
                <div style={{ maxHeight: 'calc(100vh - 220px)', overflowY: 'auto', paddingRight: 8 }}>
                  <List
                    dataSource={timeline}
                    renderItem={item => (
                      <List.Item>
                        <List.Item.Meta
                          title={(
                            <Space>
                              <Text strong>{item.title}</Text>
                              {item.risk ? <Tag color={riskColor(item.risk)}>{item.risk}</Tag> : null}
                            </Space>
                          )}
                          description={(
                            <Space direction="vertical" style={{ width: '100%' }}>
                              {item.command ? <Text code>{item.command}</Text> : null}
                              {item.content ? <Paragraph style={{ whiteSpace: 'pre-wrap', marginBottom: 0 }}>{item.content}</Paragraph> : null}
                            </Space>
                          )}
                        />
                      </List.Item>
                    )}
                  />
                  <div ref={timelineEndRef} />
                </div>
              )}
            </Card>
          </Col>
        </Row>
      </Spin>

      <Modal
        title={t('confirm.title')}
        open={Boolean(currentPendingCommand)}
        onOk={confirmPendingCommand}
        onCancel={cancelPendingCommand}
        okText={t('confirm.ok')}
        cancelText={tc('actions.cancel')}
      >
        <Space direction="vertical">
          <Alert type="warning" showIcon message={t('confirm.warning')} />
          <Text code>{currentPendingCommand?.command}</Text>
          <Text>{currentPendingCommand?.purpose}</Text>
          {pendingCommands.length > 1 ? <Text type="secondary">{t('confirm.remaining', { count: pendingCommands.length - 1 })}</Text> : null}
        </Space>
      </Modal>
    </div>
  );
};

export default PodArthas;
