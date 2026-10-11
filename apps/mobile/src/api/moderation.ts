/**
 * The moderation endpoints of the Knot HTTP API: content reports and user blocks
 * (KNOT-017a).
 *
 * Blocking is mutual hiding plus write prevention (KNOT-ADR-060). The server
 * applies both: a blocked user's content is filtered out of the blocker's reads,
 * and a write that targets either side of a block is refused with the machine
 * code `blocked`. The client does not filter content itself — it simply renders
 * the filtered lists the API returns.
 *
 * Everything here goes through the shared transport in `client.ts`, so error
 * handling is identical to the other endpoints: a failure is always an `ApiError`
 * carrying the server's machine-readable code.
 */
import { ApiError, describeError, request } from './client';

/** The kinds of content that can be reported. The set matches the server's schema. */
export type ReportEntityType =
  'story' | 'version' | 'comment' | 'bridge' | 'inquiry' | 'inquiry_answer';

/** Why content was reported. The set is closed: the server refuses anything else. */
export type ReportCategory =
  'harassment' | 'hate_speech' | 'misinformation' | 'spam' | 'sensitive_content' | 'other';

/** The six categories in display order, with the label the picker shows. */
export const REPORT_CATEGORIES: readonly {
  readonly value: ReportCategory;
  readonly label: string;
}[] = [
  { value: 'harassment', label: 'Harassment' },
  { value: 'hate_speech', label: 'Hate speech' },
  { value: 'misinformation', label: 'Misinformation' },
  { value: 'spam', label: 'Spam' },
  { value: 'sensitive_content', label: 'Sensitive content' },
  { value: 'other', label: 'Other' },
];

/** The longest a report reason may be, mirroring the server's rule. */
export const MAX_REPORT_REASON_LENGTH = 1000;

/** The error code the server returns when a write targets blocked content. */
export const BLOCKED_ERROR_CODE = 'blocked';

/** The message shown when the server refuses a write because of a block. */
export const BLOCKED_MESSAGE = 'You cannot interact with this content.';

/**
 * Returns the first client-side problem with a report's reason, or undefined when
 * it is acceptable.
 *
 * A reason is required only for the "other" category, where the moderator has no
 * category to go on; the server enforces the same rule, so this only avoids a
 * needless round trip. It is pure and tested without rendering React Native.
 */
export function reportReasonError(category: ReportCategory, reason: string): string | undefined {
  const trimmed = reason.trim();
  if (category === 'other' && trimmed === '') {
    return 'A reason is required when the category is Other.';
  }
  if (trimmed.length > MAX_REPORT_REASON_LENGTH) {
    return `The reason must be at most ${MAX_REPORT_REASON_LENGTH} characters.`;
  }
  return undefined;
}

/** A report as returned by the API. */
export type Report = {
  readonly id: string;
  readonly entity_type: ReportEntityType;
  readonly entity_id: string;
  readonly category: ReportCategory;
  /** The free-text reason, or null when none was given. */
  readonly reason: string | null;
  readonly created_at: string;
};

/** The request body for POST /reports. */
export type CreateReportPayload = {
  readonly entity_type: ReportEntityType;
  readonly entity_id: string;
  readonly category: ReportCategory;
  readonly reason?: string;
};

/** One page of GET /reports/mine. */
export type ReportPage = {
  readonly reports: readonly Report[];
  readonly next_cursor: string;
};

/** A blocked user as the block list names them. */
export type BlockedUser = {
  readonly id: string;
  readonly display_name: string;
  /** A path on the API, or null when they have no avatar. */
  readonly avatar_url: string | null;
  readonly role: string;
};

/** One entry of GET /blocks/mine. */
export type BlockEntry = {
  readonly user: BlockedUser;
  readonly created_at: string;
};

/** One page of GET /blocks/mine. */
export type BlockPage = {
  readonly blocks: readonly BlockEntry[];
  readonly next_cursor: string;
};

/** The report endpoints. */
export const reportsApi = {
  /**
   * POST /reports — files a report against one entity.
   *
   * The reporter is taken from `token` by the server; it is never sent in the
   * body. A second report by the same user on the same entity is a 409.
   */
  createReport(payload: CreateReportPayload, token: string): Promise<Report> {
    return request<Report>('/reports', { method: 'POST', body: payload, token });
  },

  /**
   * GET /reports/mine — the caller's own reports, newest first, cursor-paginated.
   */
  listMyReports(token: string, cursor?: string, limit?: number): Promise<ReportPage> {
    const query = new URLSearchParams();
    if (cursor !== undefined && cursor !== '') {
      query.set('cursor', cursor);
    }
    if (limit !== undefined) {
      query.set('limit', String(limit));
    }
    const suffix = query.toString() === '' ? '' : `?${query.toString()}`;
    return request<ReportPage>(`/reports/mine${suffix}`, { method: 'GET', token });
  },
};

/** The block endpoints. */
export const blocksApi = {
  /**
   * POST /blocks/{user_id} — blocks a user. Idempotent: blocking an
   * already-blocked user is still a 204.
   */
  block(userId: string, token: string): Promise<void> {
    return request<void>(`/blocks/${encodeURIComponent(userId)}`, { method: 'POST', token });
  },

  /**
   * DELETE /blocks/{user_id} — unblocks a user. Idempotent.
   */
  unblock(userId: string, token: string): Promise<void> {
    return request<void>(`/blocks/${encodeURIComponent(userId)}`, { method: 'DELETE', token });
  },

  /**
   * GET /blocks/mine — the caller's own block list, newest first,
   * cursor-paginated.
   */
  listBlocks(token: string, cursor?: string, limit?: number): Promise<BlockPage> {
    const query = new URLSearchParams();
    if (cursor !== undefined && cursor !== '') {
      query.set('cursor', cursor);
    }
    if (limit !== undefined) {
      query.set('limit', String(limit));
    }
    const suffix = query.toString() === '' ? '' : `?${query.toString()}`;
    return request<BlockPage>(`/blocks/mine${suffix}`, { method: 'GET', token });
  },
};

/**
 * Renders a submission failure as a message safe to show a user, mapping the
 * server's `blocked` code to a friendly explanation and falling back to the
 * shared error rendering otherwise.
 */
export function describeModerationError(error: unknown): string {
  if (error instanceof ApiError && error.code === BLOCKED_ERROR_CODE) {
    return BLOCKED_MESSAGE;
  }
  return describeError(error);
}
