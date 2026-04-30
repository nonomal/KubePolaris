export type ArthasRisk = 'low' | 'medium' | 'high';

export interface ArthasJavaProcess {
  pid: string;
  mainClass: string;
}

export interface ArthasStatus {
  enabled: boolean;
  attached: boolean;
  container: string;
  processes: ArthasJavaProcess[];
  arthasAvailable: boolean;
  downloadable: boolean;
  launcher?: string;
  message?: string;
  autoExecLowRisk: boolean;
  maxOutputBytes: number;
  sessionTimeoutMs: number;
}

export interface ArthasCommandDecision {
  risk: ArthasRisk;
  autoExecutable: boolean;
  requiresConfirmation: boolean;
  reason: string;
  maxOutputBytes: number;
}

export interface ArthasPlannedCommand {
  id: string;
  command: string;
  purpose: string;
  risk: ArthasRisk;
  requiresConfirmation: boolean;
}

export interface ArthasPlan {
  intent: string;
  skill: string;
  reasoning: string;
  commands: ArthasPlannedCommand[];
  expectedSignals: string[];
  report?: string;
}

export interface ArthasExecResult {
  stdout: string;
  stderr?: string;
  exitCode: number;
}

export interface ArthasDiagnosisReport {
  conclusion: string;
  evidence: string[];
  possibleCauses: string[];
  recommendations: string[];
}

export interface ArthasSessionResponse {
  container: string;
  pid: string;
  status: ArthasStatus;
}

export interface ArthasWSEvent {
  type: 'connected' | 'plan' | 'confirmation_required' | 'command_result' | 'diagnosis_report' | 'done' | 'error';
  data?: ArthasPlan | ArthasPlannedCommand | ArthasExecResult | ArthasDiagnosisReport;
  message?: string;
  command?: string;
  decision?: ArthasCommandDecision;
}
