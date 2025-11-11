import { http, HttpResponse } from 'msw';

import type { Agreement, Dispute, Paginated, ReferralMatch, ReferralRequest } from '@/types';
import { baseFixtures, loadFixtures, persistFixtures } from '@/mocks/fixtures';

const db = loadFixtures();

const paginate = <T>(items: T[], page: number, pageSize: number): Paginated<T> => {
  const start = (page - 1) * pageSize;
  const end = start + pageSize;
  const slice = items.slice(start, end);
  return {
    items: slice,
    total: items.length,
    page,
    pageSize,
  };
};

const sortData = <T>(items: T[], key: string, order: 'asc' | 'desc') => {
  const sorted = [...items];
  sorted.sort((a, b) => {
    const aValue = (a as Record<string, unknown>)[key];
    const bValue = (b as Record<string, unknown>)[key];

    if (typeof aValue === 'number' && typeof bValue === 'number') {
      return order === 'asc' ? aValue - bValue : bValue - aValue;
    }

    const aString = String(aValue ?? '');
    const bString = String(bValue ?? '');
    return order === 'asc' ? aString.localeCompare(bString) : bString.localeCompare(aString);
  });
  return sorted;
};

const generateId = (prefix: string) => {
  const uid = typeof crypto !== 'undefined' && 'randomUUID' in crypto
    ? crypto.randomUUID()
    : Math.random().toString(36).slice(2, 10);
  return `${prefix}-${uid}`;
};

export const handlers = [
  http.post('/auth/login', async ({ request }) => {
    const body = await request.json();
    const { email, password } = body as { email: string; password: string };

    const agent = db.agents.find((item) => item.email === email);
    if (!agent || password !== 'password') {
      return HttpResponse.json({ message: '账号或密码错误' }, { status: 401 });
    }

    return HttpResponse.json({ token: `mock-token-${agent.id}` });
  }),

  http.get('/api/me', () => {
    const agent = db.agents[0];
    return HttpResponse.json(agent);
  }),

  http.get('/api/brokers', ({ request }) => {
    const url = new URL(request.url);
    const limit = Number(url.searchParams.get('limit') ?? 100);
    const items = db.brokers.slice(0, Math.min(db.brokers.length, limit));
    return HttpResponse.json({ items, total: db.brokers.length });
  }),

  http.get('/api/referrals', ({ request }) => {
    const url = new URL(request.url);
    const status = url.searchParams.get('status');
    const region = url.searchParams.get('region');
    const dealType = url.searchParams.get('dealType');
    const page = Number(url.searchParams.get('page') ?? 1);
    const pageSize = Number(url.searchParams.get('pageSize') ?? 10);
    const sortKey = url.searchParams.get('sortKey') ?? 'createdAt';
    const sortOrder = (url.searchParams.get('sortOrder') as 'asc' | 'desc') ?? 'desc';

    let items = [...db.referrals];
    if (status) {
      items = items.filter((item) => item.status === status);
    }
    if (region) {
      items = items.filter((item) => item.region.includes(region));
    }
    if (dealType) {
      items = items.filter((item) => item.dealType === dealType);
    }

    items = sortData(items, sortKey, sortOrder);
    const payload = paginate(items, page, pageSize);
    return HttpResponse.json(payload);
  }),

  http.post('/api/referrals', async ({ request }) => {
    const body = await request.json();
    const payload = body as Pick<ReferralRequest, 'region' | 'priceMin' | 'priceMax' | 'propertyType' | 'dealType' | 'languages' | 'slaHours'>;
    const nowISO = new Date().toISOString();
    const created: ReferralRequest = {
      ...payload,
      id: generateId('rr'),
      creatorAgentId: db.agents[0].id,
      status: 'open',
      createdAt: nowISO,
      updatedAt: nowISO,
    };
    db.referrals.unshift(created);
    persistFixtures(db);
    return HttpResponse.json(created, { status: 201 });
  }),

  http.post('/api/referrals/:id/cancel', async ({ params, request }) => {
    const requestId = params.id as string;
    const body = await request.json();
    const payload = body as { reason?: string };
    const target = db.referrals.find((item) => item.id === requestId);
    if (!target) {
      return HttpResponse.json({ message: 'not found' }, { status: 404 });
    }
    target.status = 'cancelled';
    target.cancelReason = payload.reason ?? undefined;
    target.updatedAt = new Date().toISOString();
    persistFixtures(db);
    return HttpResponse.json(target);
  }),

  http.get('/api/referrals/:id/matches', ({ params }) => {
    const requestId = params.id as string;
    const matches = db.matches.filter((item) => item.requestId === requestId);
    return HttpResponse.json({ items: matches });
  }),

  http.post('/api/referrals/:id/matches', async ({ params, request }) => {
    const requestId = params.id as string;
    const body = await request.json();
    const payload = body as { candidateAgentId?: string; score?: number; state?: ReferralMatch['state'] };
    if (!payload.candidateAgentId) {
      return HttpResponse.json({ message: 'candidateAgentId is required' }, { status: 400 });
    }

    const exists = db.matches.some((item) => item.requestId === requestId && item.candidateAgentId === payload.candidateAgentId);
    if (exists) {
      return HttpResponse.json({ message: 'match already exists' }, { status: 409 });
    }

    const created: ReferralMatch = {
      id: generateId('match'),
      requestId,
      candidateAgentId: payload.candidateAgentId,
      state: payload.state ?? 'invited',
      score: payload.score ?? 0,
      createdAt: new Date().toISOString(),
    };
    db.matches.unshift(created);
    persistFixtures(db);
    return HttpResponse.json(created, { status: 201 });
  }),

  http.get('/api/matches', () => {
    return HttpResponse.json({ items: db.matches });
  }),

  http.patch('/api/referrals/:id/matches/:matchId', async ({ params, request }) => {
    const requestId = params.id as string;
    const matchId = params.matchId as string;
    const body = await request.json();
    const payload = body as { state?: ReferralMatch['state'] };
    const match = db.matches.find((item) => item.id === matchId && item.requestId === requestId);
    if (!match) {
      return HttpResponse.json({ message: 'not found' }, { status: 404 });
    }
    if (payload.state !== 'accepted' && payload.state !== 'declined') {
      return HttpResponse.json({ message: 'state must be accepted or declined' }, { status: 400 });
    }
    match.state = payload.state;
    if (payload.state === 'accepted') {
      const agreement: Agreement = {
        id: generateId('ag'),
        requestId: match.requestId,
        referrerBrokerId: db.brokers[0]?.id ?? 'b1',
        refereeBrokerId: db.brokers[1]?.id ?? 'b2',
        feeRate: 30,
        protectDays: 90,
        effectiveAt: new Date().toISOString(),
      };
      match.agreement = agreement;
      db.agreements.unshift(agreement);
    }
    persistFixtures(db);
    return HttpResponse.json(match);
  }),

  http.get('/api/disputes', ({ request }) => {
    const url = new URL(request.url);
    const agreementId = url.searchParams.get('agreementId');
    let items = [...db.disputes];
    if (agreementId) {
      items = items.filter((item) => item.agreementId === agreementId);
    }
    return HttpResponse.json({ items });
  }),

  http.post('/api/disputes', async ({ request }) => {
    const body = await request.json();
    const payload = body as { agreementId?: string };
    if (!payload.agreementId) {
      return HttpResponse.json({ message: 'agreementId is required' }, { status: 400 });
    }
    const agreement = db.agreements.find((item) => item.id === payload.agreementId);
    if (!agreement) {
      return HttpResponse.json({ message: 'agreement not found' }, { status: 404 });
    }
    const created: Dispute = {
      id: generateId('disp'),
      agreementId: agreement.id,
      status: 'under_review',
      createdAt: new Date().toISOString(),
      updatedAt: new Date().toISOString(),
    };
    db.disputes.unshift(created);
    persistFixtures(db);
    return HttpResponse.json(created, { status: 201 });
  }),

  http.patch('/api/disputes/:id', async ({ params, request }) => {
    const id = params.id as string;
    const body = await request.json();
    const payload = body as { status?: Dispute['status'] };
    const dispute = db.disputes.find((item) => item.id === id);
    if (!dispute) {
      return HttpResponse.json({ message: 'not found' }, { status: 404 });
    }
    if (payload.status !== 'resolved') {
      return HttpResponse.json({ message: 'status must be resolved' }, { status: 400 });
    }
    dispute.status = 'resolved';
    dispute.updatedAt = new Date().toISOString();
    dispute.resolvedAt = dispute.updatedAt;
    persistFixtures(db);
    return HttpResponse.json(dispute);
  }),

  http.get('/api/agreements', ({ request }) => {
    const url = new URL(request.url);
    const page = Number(url.searchParams.get('page') ?? 1);
    const pageSize = Number(url.searchParams.get('pageSize') ?? 10);

    const payload = paginate(db.agreements, page, pageSize);
    return HttpResponse.json(payload);
  }),

  http.post('/api/agreements', async ({ request }) => {
    const body = await request.json();
    const payload = body as Agreement;
    const created: Agreement = {
      id: generateId('ag'),
      requestId: payload.requestId,
      referrerBrokerId: payload.referrerBrokerId,
      refereeBrokerId: payload.refereeBrokerId,
      feeRate: payload.feeRate,
      protectDays: payload.protectDays,
      effectiveAt: payload.effectiveAt ?? new Date().toISOString(),
    };
    db.agreements.unshift(created);
    persistFixtures(db);
    return HttpResponse.json(created, { status: 201 });
  }),

  http.get('/api/events', ({ request }) => {
    const url = new URL(request.url);
    const page = Number(url.searchParams.get('page') ?? 1);
    const pageSize = Number(url.searchParams.get('pageSize') ?? 20);
    const sorted = [...db.events].sort(
      (a, b) => new Date(b.at).getTime() - new Date(a.at).getTime(),
    );
    return HttpResponse.json(paginate(sorted, page, pageSize));
  }),

  http.get('/api/brokers/:id', ({ params }) => {
    const broker = db.brokers.find((item) => item.id === params.id);
    if (!broker) {
      return HttpResponse.json({ message: '未找到经纪公司' }, { status: 404 });
    }
    return HttpResponse.json(broker);
  }),

  http.post('/__reset', async ({ request }) => {
    const body = (await request.json()) as { seed?: string } | null;
    if (body?.seed) {
      Object.assign(db, JSON.parse(JSON.stringify(baseFixtures)));
      persistFixtures(db);
    }
    return HttpResponse.json({ ok: true });
  }),
];
