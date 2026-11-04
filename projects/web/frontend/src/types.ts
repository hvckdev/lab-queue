export interface NamedItem { id: number; name: string }
export type Group = NamedItem;
export type Subject = NamedItem;

export interface User {
  id: number;
  name: string;
  email: string;
  role: 'student' | 'teacher' | 'admin';
  group_id: number | null;
}

export interface Session {
  id: number;
  teacher_id: number;
  subject_id: number;
  subject_name: string;
  group_id: number;
  group_name: string;
  lab_title: string;
  starts_at: string;
  ends_at: string;
  registration_closed: boolean;
  finished: boolean;
  registration_open: boolean;
  active_count: number;
}

export type EntryStatus = 'waiting' | 'called' | 'cancelled' | 'completed' | 'skipped';
export interface Entry {
  id: number;
  session_id: number;
  student_id: number;
  student_name: string;
  ticket_number: number;
  status: EntryStatus;
  position: number;
  created_at: string;
}

export interface QueueSnapshot {
  session: Session;
  entries: Entry[];
  my_entry: Entry | null;
  next_position: number;
}

export interface LoginInput { email: string; password: string }
export interface RegisterInput extends LoginInput { name: string; group_id: number }
export interface AdminUserInput extends LoginInput { name: string; role: User['role']; group_id: number | null }
export interface CreateSessionInput {
  subject_id: number;
  group_id: number;
  lab_title: string;
  starts_at: string;
  ends_at: string;
}
export interface Notice { id: number; message: string; tone: 'success' | 'error' }

export interface LabQueueController {
  user: User | null;
  booting: boolean;
  groups: Group[];
  subjects: Subject[];
  sessions: Session[];
  selectedId: number | null;
  queue: QueueSnapshot | null;
  subjectFilter: string;
  groupFilter: string;
  loading: boolean;
  busy: boolean;
  authBusy: boolean;
  authError: string | null;
  bootstrapError: string | null;
  syncError: string | null;
  lastUpdated: Date | null;
  notice: Notice | null;
  pending: { join: boolean; callNext: boolean; create: boolean };
  setSubjectFilter(value: string): void;
  setGroupFilter(value: string): void;
  selectSession(id: number): void;
  clearNotice(): void;
  endExpiredSession(userId: number): void;
  retryBootstrap(): void;
  refresh(): Promise<void>;
  login(input: LoginInput): Promise<boolean>;
  register(input: RegisterInput): Promise<boolean>;
  logout(): Promise<boolean>;
  createSession(input: CreateSessionInput): Promise<Session | null>;
  joinQueue(): Promise<boolean>;
  callNext(): Promise<boolean>;
  closeRegistration(): Promise<boolean>;
  finishSession(): Promise<boolean>;
  cancelEntry(entryId: number): Promise<boolean>;
  completeEntry(entryId: number): Promise<boolean>;
  skipEntry(entryId: number): Promise<boolean>;
}
