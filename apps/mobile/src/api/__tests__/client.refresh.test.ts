import {
  ApiError,
  configureSessionBridge,
  fetchWithRefresh,
  refreshAccessToken,
  request,
  type AuthResponse,
} from '../client';

/**
 * The refresh-on-401 policy.
 *
 * These tests fake `fetch`, because what is under test is *what the client sends and
 * how many times it sends it* — the interesting cases are a second attempt with a
 * rotated token, and several requests sharing one refresh. A real HTTP call would
 * test the server instead.
 */

const USER = {
  id: '11111111-1111-1111-1111-111111111111',
  email: 'teller@example.com',
  display_name: 'Teller',
  preferred_languages: ['eng'],
  approximate_location: '',
  phone: '',
  created_at: '2026-10-09T00:00:00Z',
  email_verified_at: '2026-10-09T00:00:01Z',
  avatar_url: '',
};

type Responder = (url: string, init?: RequestInit) => Promise<Response>;

function jsonResponse(status: number, body: unknown): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    text: () => Promise.resolve(JSON.stringify(body)),
    json: () => Promise.resolve(body),
  } as unknown as Response;
}

/** Installs a `fetch` that answers by URL, and returns the mock. */
function mockFetch(responder: Responder): jest.Mock {
  const mock = jest.fn((url: string, init?: RequestInit) => responder(url, init));
  global.fetch = mock as unknown as typeof fetch;

  return mock;
}

/** The spies a test asserts on. */
type BridgeSpies = {
  readonly getRefreshToken: jest.Mock<string | null, []>;
  readonly onTokensRefreshed: jest.Mock<void, [AuthResponse]>;
  readonly onSessionExpired: jest.Mock<void, []>;
};

/** A bridge whose refresh token is spent through `fetch`. */
function makeBridge(overrides: Partial<BridgeSpies> = {}): BridgeSpies {
  const bridge: BridgeSpies = {
    getRefreshToken: overrides.getRefreshToken ?? jest.fn(() => 'session-id.secret'),
    onTokensRefreshed: overrides.onTokensRefreshed ?? jest.fn(),
    onSessionExpired: overrides.onSessionExpired ?? jest.fn(),
  };

  configureSessionBridge(bridge);

  return bridge;
}

const REFRESHED: AuthResponse = {
  access_token: 'second-access-token',
  refresh_token: 'session-id.second-secret',
  expires_in: 900,
  user: USER,
};

describe('fetchWithRefresh', () => {
  afterEach(() => {
    configureSessionBridge(null);
    jest.restoreAllMocks();
  });

  it('returns a successful response untouched', async () => {
    const fetchMock = mockFetch(() => Promise.resolve(jsonResponse(200, { ok: true })));
    makeBridge();

    const response = await fetchWithRefresh('/stories', { method: 'GET', token: 'first' });

    expect(response.status).toBe(200);
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('refreshes once and retries the request with the new token', async () => {
    const seen: string[] = [];
    const fetchMock = mockFetch((url, init) => {
      const authorization = (init?.headers as Record<string, string> | undefined)?.Authorization;

      if (url.endsWith('/auth/refresh')) {
        return Promise.resolve(jsonResponse(200, REFRESHED));
      }

      seen.push(authorization ?? '');
      if (seen.length === 1) {
        return Promise.resolve(jsonResponse(401, { error: { code: 'unauthorized' } }));
      }

      return Promise.resolve(jsonResponse(200, { stories: [] }));
    });
    const bridge = makeBridge();

    const response = await fetchWithRefresh('/stories', { method: 'GET', token: 'first' });

    expect(response.status).toBe(200);
    // The retry carries the rotated token, not the one that failed.
    expect(seen).toEqual(['Bearer first', 'Bearer second-access-token']);
    expect(bridge.onTokensRefreshed).toHaveBeenCalledWith(REFRESHED);
    expect(bridge.onSessionExpired).not.toHaveBeenCalled();
    expect(fetchMock).toHaveBeenCalledTimes(3);
  });

  it('does not retry a 401 on a request that carried no token', async () => {
    const fetchMock = mockFetch(() => Promise.resolve(jsonResponse(401, {})));
    const bridge = makeBridge();

    const response = await fetchWithRefresh('/auth/login', { method: 'POST' });

    expect(response.status).toBe(401);
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(bridge.onTokensRefreshed).not.toHaveBeenCalled();
  });

  it('retries only once when the retry is refused too', async () => {
    const fetchMock = mockFetch((url) => {
      if (url.endsWith('/auth/refresh')) {
        return Promise.resolve(jsonResponse(200, REFRESHED));
      }

      return Promise.resolve(jsonResponse(401, { error: { code: 'unauthorized' } }));
    });
    makeBridge();

    const response = await fetchWithRefresh('/stories', { method: 'GET', token: 'first' });

    expect(response.status).toBe(401);
    // original + refresh + one retry, and never a third attempt.
    expect(fetchMock).toHaveBeenCalledTimes(3);
  });

  it('ends the session when the server refuses the refresh token', async () => {
    const fetchMock = mockFetch((url) => {
      if (url.endsWith('/auth/refresh')) {
        return Promise.resolve(jsonResponse(401, { error: { code: 'invalid_refresh_token' } }));
      }

      return Promise.resolve(jsonResponse(401, { error: { code: 'unauthorized' } }));
    });
    const bridge = makeBridge();

    const response = await fetchWithRefresh('/stories', { method: 'GET', token: 'first' });

    expect(response.status).toBe(401);
    expect(bridge.onSessionExpired).toHaveBeenCalledTimes(1);
    expect(bridge.onTokensRefreshed).not.toHaveBeenCalled();
    // The failed request is not re-sent with no credential.
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it('shares one refresh between concurrent 401s', async () => {
    let refreshCalls = 0;
    const fetchMock = mockFetch((url, init) => {
      if (url.endsWith('/auth/refresh')) {
        refreshCalls += 1;
        return Promise.resolve(jsonResponse(200, REFRESHED));
      }

      const authorization = (init?.headers as Record<string, string> | undefined)?.Authorization;
      if (authorization === 'Bearer second-access-token') {
        return Promise.resolve(jsonResponse(200, { ok: true }));
      }

      return Promise.resolve(jsonResponse(401, { error: { code: 'unauthorized' } }));
    });
    const bridge = makeBridge();

    const responses = await Promise.all([
      fetchWithRefresh('/a', { method: 'GET', token: 'first' }),
      fetchWithRefresh('/b', { method: 'GET', token: 'first' }),
      fetchWithRefresh('/c', { method: 'GET', token: 'first' }),
    ]);

    expect(responses.map((response) => response.status)).toEqual([200, 200, 200]);
    // Three requests, one rotation: reusing a spent refresh token would be treated
    // as theft by the server (KNOT-ADR-085).
    expect(refreshCalls).toBe(1);
    expect(bridge.onTokensRefreshed).toHaveBeenCalledTimes(1);
    expect(fetchMock).toHaveBeenCalledTimes(7);
  });

  it('reports a network failure as an ApiError with status 0', async () => {
    mockFetch(() => Promise.reject(new Error('network down')));
    makeBridge();

    await expect(fetchWithRefresh('/stories', { method: 'GET', token: 'first' })).rejects.toThrow(
      expect.objectContaining({ status: 0, code: 'network_error' }) as ApiError,
    );
  });

  it('returns null from refreshAccessToken when there is no bridge', async () => {
    configureSessionBridge(null);

    await expect(refreshAccessToken()).resolves.toBeNull();
  });

  it('returns null from refreshAccessToken when there is no stored token', async () => {
    mockFetch(() => Promise.resolve(jsonResponse(200, REFRESHED)));
    makeBridge({ getRefreshToken: jest.fn(() => null) });

    await expect(refreshAccessToken()).resolves.toBeNull();
  });

  it('leaves the session alone when the refresh could not be attempted', async () => {
    mockFetch((url) => {
      if (url.endsWith('/auth/refresh')) {
        return Promise.reject(new Error('offline'));
      }

      return Promise.resolve(jsonResponse(401, { error: { code: 'unauthorized' } }));
    });
    const bridge = makeBridge();

    const response = await fetchWithRefresh('/stories', { method: 'GET', token: 'first' });

    expect(response.status).toBe(401);
    // Being offline is not being signed out.
    expect(bridge.onSessionExpired).not.toHaveBeenCalled();
  });
});

describe('request', () => {
  afterEach(() => {
    configureSessionBridge(null);
    jest.restoreAllMocks();
  });

  it('throws an ApiError carrying the server code', async () => {
    mockFetch(() =>
      Promise.resolve(
        jsonResponse(403, { error: { code: 'email_not_verified', message: 'verify' } }),
      ),
    );

    await expect(request('/auth/login', { method: 'POST' })).rejects.toMatchObject({
      status: 403,
      code: 'email_not_verified',
      message: 'verify',
    });
  });
});
