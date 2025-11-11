import type { Agent, Agreement, Broker, Dispute, ReferralMatch, ReferralRequest, TimelineEvent } from '@/types';

export interface DemoDatabase {
  agents: Agent[];
  brokers: Broker[];
  referrals: ReferralRequest[];
  matches: ReferralMatch[];
  agreements: Agreement[];
  events: TimelineEvent[];
  disputes: Dispute[];
}

const now = new Date();

const brokers: Broker[] = [
  { id: 'b1', name: 'Metro Realty Group', fein: '45-6789123', verified: true },
  { id: 'b2', name: 'Harbor Estates', fein: '11-2233445', verified: true },
  { id: 'b3', name: 'Skyline Partners', fein: '98-7654321', verified: false },
];

const agents: Agent[] = [
  {
    id: 'a1',
    fullName: 'Alex Agent',
    email: 'alex.agent@example.com',
    phone: '+1-212-555-1200',
    languages: ['English', '中文'],
    brokerId: 'b1',
    rating: 4.8,
    role: 'broker_admin',
  },
  {
    id: 'a2',
    fullName: 'Maria Perez',
    email: 'maria.perez@example.com',
    phone: '+1-917-555-2211',
    languages: ['English', 'Español'],
    brokerId: 'b2',
    rating: 4.6,
    role: 'agent',
  },
  {
    id: 'a3',
    fullName: 'Daniel Kim',
    email: 'daniel.kim@example.com',
    phone: '+1-646-555-6677',
    languages: ['English', '한국어'],
    brokerId: 'b3',
    rating: 4.7,
    role: 'agent',
  },
];

const referralStatuses: ReferralRequest['status'][] = [
  'open',
  'matched',
  'signed',
  'in_progress',
  'closed',
  'disputed',
  'cancelled',
];

const propertyTypes: ReferralRequest['propertyType'][] = ['condo', 'coop', 'sfh', 'rent'];
const dealTypes: ReferralRequest['dealType'][] = ['buy', 'sell', 'rent'];
const regionsPool = ['曼哈顿', '皇后区', '布鲁克林', '长岛', '新泽西'];

const referrals: ReferralRequest[] = Array.from({ length: 12 }).map((_, index) => {
  const createdAt = new Date(now.getTime() - index * 36 * 60 * 60 * 1000).toISOString();
  const status = referralStatuses[index % referralStatuses.length];
  const propertyType = propertyTypes[index % propertyTypes.length];
  const dealType = dealTypes[index % dealTypes.length];
  const region = [regionsPool[index % regionsPool.length]];
  const languages = agents[index % agents.length].languages;

  return {
    id: `rr-${index + 1}`,
    creatorAgentId: agents[0].id,
    region,
    priceMin: 400000 + index * 25000,
    priceMax: 600000 + index * 30000,
    propertyType,
    dealType,
    languages,
    slaHours: 24 + (index % 4) * 12,
    status,
    cancelReason: status === 'cancelled' ? '客户取消需求' : undefined,
    createdAt,
    updatedAt: createdAt,
  };
});

const matches: ReferralMatch[] = referrals.slice(0, 6).map((referral, index) => ({
  id: `match-${index + 1}`,
  requestId: referral.id,
  candidateAgentId: agents[(index + 1) % agents.length].id,
  score: 0.75 + index * 0.03,
  state: index % 3 === 0 ? 'accepted' : index % 3 === 1 ? 'invited' : 'declined',
  createdAt: new Date(now.getTime() - index * 12 * 60 * 60 * 1000).toISOString(),
}));

const agreements: Agreement[] = referrals.slice(0, 4).map((referral, index) => ({
  id: `ag-${index + 1}`,
  requestId: referral.id,
  referrerBrokerId: brokers[0].id,
  refereeBrokerId: brokers[(index + 1) % brokers.length].id,
  feeRate: 25 + index * 2,
  protectDays: 90 + index * 10,
  effectiveAt: new Date(now.getTime() - index * 5 * 24 * 60 * 60 * 1000).toISOString(),
}));

const eventTypes: TimelineEvent['type'][] = ['lead_shared', 'toured', 'offer', 'contract', 'close'];

const events: TimelineEvent[] = agreements.flatMap((agreement, agreementIndex) =>
  eventTypes.map((type, idx) => ({
    id: `event-${agreementIndex + 1}-${idx + 1}`,
    agreementId: agreement.id,
    type,
    at: new Date(now.getTime() - (agreementIndex * 5 + idx) * 12 * 60 * 60 * 1000).toISOString(),
    payload: { note: `${type} event for ${agreement.id}` },
  })),
);

const disputes: Dispute[] = agreements.slice(0, 2).map((agreement, index) => ({
  id: `disp-${index + 1}`,
  agreementId: agreement.id,
  status: index === 0 ? 'under_review' : 'resolved',
  createdAt: new Date(now.getTime() - (index+1) * 24 * 60 * 60 * 1000).toISOString(),
  updatedAt: new Date(now.getTime() - index * 12 * 60 * 60 * 1000).toISOString(),
  resolvedAt: index === 0 ? undefined : new Date(now.getTime() - index * 12 * 60 * 60 * 1000).toISOString(),
}));

export const baseFixtures: DemoDatabase = {
  agents,
  brokers,
  referrals,
  matches,
  agreements,
  events,
  disputes,
};

export const STORAGE_KEY = 'arn-demo-fixtures';

const clone = <T>(value: T): T => JSON.parse(JSON.stringify(value));

export const loadFixtures = (): DemoDatabase => {
  try {
    const saved = localStorage.getItem(STORAGE_KEY);
    if (saved) {
      const parsed = JSON.parse(saved) as DemoDatabase;
      if (!parsed.disputes) {
        parsed.disputes = clone(baseFixtures.disputes);
      }
      if (parsed.referrals) {
        parsed.referrals = parsed.referrals.map((item) => ({
          ...item,
          updatedAt: item.updatedAt ?? item.createdAt,
        }));
      }
      return parsed;
    }
  } catch (error) {
    console.warn('读取本地 demo 数据失败', error);
  }
  return clone(baseFixtures);
};

export const persistFixtures = (db: DemoDatabase) => {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(db));
  } catch (error) {
    console.warn('保存 demo 数据失败', error);
  }
};
