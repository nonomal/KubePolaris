import React, { useEffect, useRef, useState } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { Button, Card, Spin, Typography } from 'antd';
import { ArrowLeftOutlined } from '@ant-design/icons';
import { create } from 'asciinema-player';
import 'asciinema-player/dist/bundle/asciinema-player.css';
import { parseApiError } from '../../utils/api';

const { Text } = Typography;

/** 带 Bearer 拉取 asciicast 并生成 blob URL（供播放器加载） */
async function fetchReplayBlobUrl(sessionId: number, signal?: AbortSignal): Promise<string> {
  const token = localStorage.getItem('token');
  const base = import.meta.env.VITE_API_BASE_URL || '/api/v1';
  const res = await fetch(`${base}/audit/terminal/sessions/${sessionId}/replay`, {
    signal,
    headers: token ? { Authorization: `Bearer ${token}` } : {},
  });
  if (!res.ok) {
    const errText = await res.text().catch(() => res.statusText);
    throw new Error(errText || `HTTP ${res.status}`);
  }
  const blob = await res.blob();
  return URL.createObjectURL(blob);
}

const TerminalReplay: React.FC = () => {
  const { sessionId } = useParams<{ sessionId: string }>();
  const navigate = useNavigate();
  const containerRef = useRef<HTMLDivElement>(null);
  const playerRef = useRef<{ dispose: () => void } | null>(null);
  const blobUrlRef = useRef<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const id = sessionId ? parseInt(sessionId, 10) : NaN;
    if (!Number.isFinite(id)) {
      setError('无效的会话 ID');
      setLoading(false);
      return;
    }

    const ac = new AbortController();
    let cancelled = false;

    (async () => {
      let blobUrl: string | null = null;
      try {
        blobUrl = await fetchReplayBlobUrl(id, ac.signal);
        blobUrlRef.current = blobUrl;
        if (cancelled) {
          URL.revokeObjectURL(blobUrl);
          blobUrlRef.current = null;
          return;
        }
        // 先结束 loading，避免 Card 处于 antd loading 遮罩时挂载播放器导致画布高度为 0 / 不可见
        setLoading(false);
        await new Promise<void>((resolve) => {
          requestAnimationFrame(() => requestAnimationFrame(() => resolve()));
        });
        if (cancelled || !containerRef.current) {
          URL.revokeObjectURL(blobUrl);
          blobUrlRef.current = null;
          return;
        }
        const player = create(blobUrl, containerRef.current, {
          autoPlay: true,
          fit: 'width',
        });
        playerRef.current = player;
      } catch (e: unknown) {
        if (e instanceof Error && e.name === 'AbortError') {
          return;
        }
        setError(parseApiError(e));
        setLoading(false);
        if (blobUrl) {
          URL.revokeObjectURL(blobUrl);
          blobUrlRef.current = null;
        }
      }
    })();

    return () => {
      cancelled = true;
      ac.abort();
      if (playerRef.current) {
        try {
          playerRef.current.dispose();
        } catch {
          /* ignore */
        }
        playerRef.current = null;
      }
      if (blobUrlRef.current) {
        URL.revokeObjectURL(blobUrlRef.current);
        blobUrlRef.current = null;
      }
    };
  }, [sessionId]);

  return (
    <div style={{ padding: 24 }}>
      <Button icon={<ArrowLeftOutlined />} type="link" onClick={() => navigate(-1)} style={{ marginBottom: 16 }}>
        返回
      </Button>
      <Card title="会话录像回放">
        {error && <Text type="danger">{error}</Text>}
        {!error && (
          <div style={{ minHeight: 400 }}>
            {loading && (
              <div style={{ textAlign: 'center', padding: 48 }}>
                <Spin size="large" />
              </div>
            )}
            <div ref={containerRef} style={{ width: '100%', minHeight: loading ? 1 : 360 }} />
          </div>
        )}
      </Card>
    </div>
  );
};

export default TerminalReplay;
