export interface Agent {
  id: string;
  fullName: string;
  email: string;
  phone: string;
  languages: string[];
  brokerId: string;
  rating: number;
  role: 'agent' | 'broker_admin' | 'client';
}

export interface Broker {
  id: string;
  name: string;
  fein: string;
  verified: boolean;
}

export type PropertyType = 'condo' | 'coop' | 'sfh' | 'rent';
export type DealType = 'buy' | 'sell' | 'rent';
export type ReferralStatus =
  | 'open'
  | 'matched'
  | 'signed'
  | 'in_progress'
  | 'closed'
  | 'disputed'
  | 'cancelled';

export interface ReferralRequest {
  id: string;
  creatorAgentId: string;
  region: string[];
  priceMin: number;
  priceMax: number;
  propertyType: PropertyType;
  dealType: DealType;
  languages: string[];
  slaHours: number;
  status: ReferralStatus;
  createdAt: string;
  updatedAt: string;
  cancelReason?: string;
}

export type ReferralMatchState = 'invited' | 'declined' | 'accepted';

export interface ReferralMatch {
  id: string;
  requestId: string;
  candidateAgentId: string;
  score: number;
  state: ReferralMatchState;
  createdAt: string;
  agreement?: Agreement;
}

export interface Agreement {
  id: string;
  requestId: string;
  referrerBrokerId: string;
  refereeBrokerId: string;
  feeRate: number;
  protectDays: number;
  effectiveAt?: string;
}

export type DisputeStatus = 'under_review' | 'resolved';

export interface Dispute {
  id: string;
  agreementId: string;
  status: DisputeStatus;
  createdAt: string;
  updatedAt: string;
  resolvedAt?: string;
}

export type TimelineEventType =
  | 'lead_shared'
  | 'toured'
  | 'offer'
  | 'contract'
  | 'close'
  | 'cancel';

export interface TimelineEvent {
  id: string;
  agreementId: string;
  type: TimelineEventType;
  at: string;
  payload?: Record<string, unknown>;
}

export type ApiResult<T> = {
  data: T;
  error?: never;
} | {
  data?: never;
  error: {
    message: string;
    status?: number;
  };
};

export interface Paginated<T> {
  items: T[];
  total: number;
  page: number;
  pageSize: number;
}
