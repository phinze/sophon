export interface Session {
  session_id: string;
  project: string;
  node_name?: string;
  started_at: string;
  stopped_at?: string;
  last_activity_at?: string;
  notification_type?: string;
  notify_message?: string;
  notified_at?: string;
  tmux_pane?: string;
  cwd?: string;
}

export interface SessionsResponse {
  active: Session[] | null;
  recent: Session[] | null;
}

export interface GlobalEvent {
  type: string;
  session_id: string;
  data?: unknown;
}

export interface NotificationEventData {
  type?: string;
  message?: string;
  title?: string;
}

export interface AskQuestionOption {
  label: string;
  description?: string;
}

export interface AskQuestion {
  header?: string;
  question: string;
  options?: AskQuestionOption[];
}

export interface AskQuestionInput {
  questions?: AskQuestion[];
}

export interface WriteInput {
  file_path?: string;
  content?: string;
}

export interface TranscriptBlock {
  type: string;
  text: string;
  summary?: string;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  input?: AskQuestionInput & WriteInput & Record<string, any>;
}

export interface TranscriptMessage {
  role: string;
  blocks?: TranscriptBlock[];
}

export interface TranscriptData {
  messages?: TranscriptMessage[];
}
